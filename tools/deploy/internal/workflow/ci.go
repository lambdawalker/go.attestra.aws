package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	awsenv "github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/aws"
)

func validateCI(o options, c credentials) error {
	if o.CI != "preview" && o.CI != "deploy" && o.CI != "publish" {
		return errors.New("CI mode must be preview, deploy, or publish")
	}
	if o.ReleaseIndex || o.MigrateFrom != "" || o.Login || o.Sso || o.Pull {
		return errors.New("CI cannot migrate, log in interactively, or pull Git changes")
	}
	if c.Access == "" || c.Secret == "" || c.Token == "" || c.Passphrase == "" {
		return errors.New("CI requires temporary AWS credentials and PULUMI_CONFIG_PASSPHRASE; configure AWS OIDC and the GitHub environment secret")
	}
	if !regexp.MustCompile(`^[0-9]{12}$`).MatchString(o.ExpectedAccount) {
		return errors.New("CI requires explicit -account with the intended AWS account")
	}
	if o.CI == "deploy" && !o.Approved {
		return errors.New("CI deployment requires -ci deploy -approve")
	}
	if o.Scope == "both" {
		return errors.New("CI All requires separate protected shared and backend jobs; use the deployment Action")
	}
	return o.validate()
}
func runCI(o options) (resultErr error) {
	c := credentials{Access: os.Getenv("AWS_ACCESS_KEY_ID"), Secret: os.Getenv("AWS_SECRET_ACCESS_KEY"), Token: os.Getenv("AWS_SESSION_TOKEN"), Passphrase: os.Getenv("PULUMI_CONFIG_PASSPHRASE")}
	if err := validateCI(o, c); err != nil {
		return err
	}
	root, err := filepath.Abs(o.Root)
	if err != nil {
		return err
	}
	o.Root = root
	if _, err := os.Stat(filepath.Join(root, "infra", "Pulumi.yaml")); err != nil {
		return errors.New("invalid repository root")
	}
	dir, err := os.MkdirTemp("", "attestra-ci-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	empty := filepath.Join(dir, "aws-config")
	if err := os.WriteFile(empty, nil, 0600); err != nil {
		return err
	}
	env, err := prepareCloudEnvironment(os.Environ(), c, o, empty)
	if err != nil {
		return err
	}
	r := &processRunner{env: env, ciApproved: o.CI == "deploy" && o.Approved}
	data, err := r.Exec(root, true, "aws", "sts", "get-caller-identity", "--output", "json", "--no-cli-pager")
	if err != nil {
		return err
	}
	var identity struct{ Account, Arn string }
	if json.Unmarshal(data, &identity) != nil || identity.Account == "" {
		return errors.New("invalid AWS identity response")
	}
	if err := verifyDeployAccount(o, "", identity.Account); err != nil {
		return err
	}
	r.reportAccount = identity.Account
	r.reportRole = roleIdentity(identity.Arn)
	if o.Scope == "shared" {
		if o.CI == "publish" {
			return errors.New("publication recovery belongs to a backend environment")
		}
		if o.Stack != "shared" {
			return errors.New("shared CI requires -stack shared")
		}
		if err := checkBucket(r, o, identity.Account); err != nil {
			return err
		}
		a := &awsenv.Client{Env: r.env, RenewalFailure: credentialRenewalFailure}
		return withSharedDeployLock(a, o, identity.Account, dir, func() error { return deploySharedStack(r, o, buildIndex) })
	}
	if o.CI != "publish" {
		op := "deploy"
		if o.CI == "preview" {
			op = "preview"
		}
		a := beginArtifactAttempt(r, r, o.Root, filepath.Join(o.Root, "infra"), o.Stack, op)
		r.reportActive = a
		defer func() { a.finish(resultErr); r.reportActive = nil }()
	}

	fmt.Printf("CI %s • account %s • stack %s • backend %s\n", o.CI, identity.Account, o.Stack, o.Backend)
	if err := checkBucket(r, o, identity.Account); err != nil {
		return err
	}
	if _, err := r.Exec(filepath.Join(root, "infra"), true, "pulumi", "stack", "select", o.Stack, "--non-interactive"); err != nil {
		return errors.New("CI cannot select existing stack; recover original state or run local setup")
	}
	if _, err := r.Exec(filepath.Join(root, "infra"), true, "pulumi", "config", "refresh", "--stack", o.Stack, "--force", "--non-interactive"); err != nil {
		return errors.New("CI could not restore encrypted configuration; nothing applied")
	}
	if o.CI == "deploy" {
		wizard := &bootstrapWizard{root: root, o: o, r: r, a: &awsenv.Client{Env: r.env, RenewalFailure: credentialRenewalFailure}}
		configData, e := r.Exec(filepath.Join(root, "infra"), true, "pulumi", "config", "--json", "--stack", o.Stack)
		if e != nil {
			return e
		}
		var config map[string]struct{ Value string }
		if json.Unmarshal(configData, &config) != nil {
			return errors.New("invalid stack configuration")
		}
		if domain := config["attestra-application:apiDomain"].Value; domain != "" {
			arn, e := wizard.output("apiCertificateArn")
			if e != nil {
				return errors.New("API certificate prerequisites missing; run the setup wizard first")
			}
			cert, e := wizard.certificateStatus(arn)
			if e != nil {
				return e
			}
			if cert.Certificate.DomainName != domain || cert.Certificate.Status != "ISSUED" {
				return errors.New("API certificate is not issued for configured apiDomain; run setup to validate DNS before deploying")
			}
		}
		if config["attestra-application:androidAssociations"].Value != "" {
			arn, e := wizard.output("appCertificateArn")
			if e != nil {
				return errors.New("Android hosting prerequisites missing; run setup first")
			}
			cert, e := wizard.certificateStatus(arn)
			if e != nil {
				return e
			}
			app, e := url.Parse(config["attestra-application:appOrigin"].Value)
			if e != nil || cert.Certificate.Status != "ISSUED" || cert.Certificate.DomainName != app.Hostname() {
				return errors.New("Android hosting certificate is not ready; run setup first")
			}
		}
		evidence, err := wizard.readSES()
		if err != nil {
			return fmt.Errorf("SES readiness check failed; run setup and choose Build, preview and deploy to complete SES/DNS prerequisites: %w", err)
		}
		if !sesReady(evidence) {
			return fmt.Errorf("SES %s is not ready (verification %s, DKIM %s); run setup and choose Build, preview and deploy to configure DNS and wait for verification; no deployment attempted", evidence.Domain, evidence.Verification, evidence.DKIM)
		}
	}
	if o.CI == "publish" {
		o.PublishOnly = true
	}
	if err := execute(r, o); err != nil {
		return err
	}
	if o.CI != "preview" {
		if err := waitAssetLinks(r, o); err != nil {
			return err
		}
		return runEnvironmentHealth(r, o)
	}
	return nil
}

func verifyDeployAccount(o options, saved, actual string) error {
	if saved != "" && actual != saved {
		return fmt.Errorf("authenticated account %s differs from saved account %s; deliberately reconfigure before continuing", actual, saved)
	}
	if o.ExpectedAccount != "" && actual != o.ExpectedAccount {
		return fmt.Errorf("authenticated account %s differs from explicit account %s; no changes authorized", actual, o.ExpectedAccount)
	}
	return nil
}

func requireCIInputs(flags []string) error {
	for _, name := range []string{"stack", "backend", "region", "account"} {
		if !containsReportFlag(flags, name) {
			return fmt.Errorf("CI requires explicit -%s; workstation defaults cannot select a CI target", name)
		}
	}
	return nil
}

func roleIdentity(arn string) string {
	if strings.Contains(arn, ":assumed-role/") {
		if i := strings.LastIndex(arn, "/"); i >= 0 {
			return arn[:i]
		}
	}
	return arn
}
