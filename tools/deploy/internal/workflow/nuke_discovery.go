package workflow

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	awsenv "github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/aws"
	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/github"
	"github.com/lambdawalker/go.infra.composer/teardown"
	"github.com/lambdawalker/go.infra.composer/teardown/pulumicli"
)

type nukeDiscovery struct {
	config    pulumicli.Config
	artifacts []teardown.Artifact
}

func discoverNukeConfig(root, repo, account string, env []string, g *github.Client) (pulumicli.Config, error) {
	d, err := collectNukeDiscovery(root, repo, account, g, func(region string) awsenv.API {
		return &awsenv.Client{Env: append(append([]string{}, env...), "AWS_REGION="+region, "AWS_DEFAULT_REGION="+region)}
	})
	return d.config, err
}

// GitHub deliberately hides inaccessible repositories behind 404. Only a valid,
// accessible complete environment listing can establish environment absence.
func nukeEnvironmentNames(g *github.Client, repo string) ([]string, error) {
	if g == nil {
		return nil, errors.New("GitHub discovery unavailable")
	}
	var all []string
	for page := 1; page <= 10000; page++ {
		var response struct{ Environments *[]struct{ Name string } }
		found, err := g.Request("GET", "/repos/"+repo+"/environments?per_page=100&page="+itoaNuke(page), nil, &response)
		if err != nil || !found || response.Environments == nil {
			return nil, errors.New("cannot verify complete GitHub environment listing")
		}
		for _, v := range *response.Environments {
			if !nukeName(v.Name) {
				return nil, errors.New("invalid GitHub environment identity")
			}
			all = append(all, v.Name)
		}
		if len(*response.Environments) < 100 {
			return all, nil
		}
	}
	return nil, errors.New("GitHub environment pagination exceeded")
}
func itoaNuke(v int) string { b, _ := json.Marshal(v); return string(b) }
func nukeName(v string) bool {
	return regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$`).MatchString(v)
}
func nukeAWSMissing(err error, codes ...string) bool {
	var e *awsenv.Error
	if !errors.As(err, &e) {
		return false
	}
	for _, c := range codes {
		if e.Code == c {
			return true
		}
	}
	return false
}

func collectNukeDiscovery(root, repo, account string, g *github.Client, api func(string) awsenv.API) (nukeDiscovery, error) {
	d := nukeDiscovery{config: pulumicli.Config{Scope: teardown.Scope{Application: "attestra", Account: account}}}
	if !regexp.MustCompile(`^[0-9]{12}$`).MatchString(account) || repo == "" {
		return d, errors.New("repository and AWS account required")
	}
	backends := map[string]string{}
	add := func(backend, region string) error {
		u, e := url.Parse(backend)
		if e != nil || u.Scheme != "s3" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil || region == "" {
			return errors.New("invalid or incomplete state backend")
		}
		if old, ok := backends[backend]; ok && old != region {
			return errors.New("conflicting backend region")
		}
		backends[backend] = region
		return nil
	}
	addExtras := func(environment, role, region string) error {
		if !nukeName(environment) {
			return errors.New("invalid environment identity")
		}
		data := map[string]string{"repository": repo, "environment": environment, "account": account, "cleanup": "true"}
		d.artifacts = append(d.artifacts, teardown.Artifact{ID: environment, Kind: "attestra_github_environment", Region: region, Data: data})
		if role != "" {
			parts := strings.Split(role, ":")
			if len(parts) != 6 || parts[2] != "iam" || parts[4] != account || !strings.HasPrefix(parts[5], "role/") {
				return errors.New("deployment role account mismatch")
			}
			d.artifacts = append(d.artifacts, teardown.Artifact{ID: role, Kind: "attestra_deployment_role", Region: region, Data: data})
		}
		return nil
	}
	paths, err := filepath.Glob(filepath.Join(root, ".attestra", "*.local.json*"))
	if err != nil {
		return d, errors.New("local discovery unavailable")
	}
	for _, path := range paths {
		name := filepath.Base(path)
		if !strings.HasPrefix(name, "bootstrap.") && !strings.HasPrefix(name, "deploy.") && !strings.HasPrefix(name, "teardown.") && !strings.HasPrefix(name, "teardown-index.") {
			continue
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return d, errors.New("local discovery record unreadable")
		}
		var p struct {
			Repository, Environment, Stack, Backend, Region, Account, RoleARN string
			Values                                                            map[string]string
		}
		if json.Unmarshal(b, &p) != nil {
			return d, errors.New("local discovery record malformed")
		}
		if p.Repository != "" && p.Repository != repo {
			continue
		}
		if p.Account == "" {
			p.Account = p.Values["account"]
		}
		if p.Account == "" {
			p.Account = p.Values["AWS_ACCOUNT_ID"]
		}
		if p.Account != "" && p.Account != account {
			return d, errors.New("local record AWS account mismatch")
		}
		if p.Backend == "" {
			p.Backend = p.Values["PULUMI_BACKEND_URL"]
		}
		if p.Backend == "" {
			p.Backend = p.Values["backend"]
		}
		if p.Region == "" {
			p.Region = p.Values["AWS_REGION"]
		}
		if p.Region == "" {
			p.Region = p.Values["region"]
		}
		if p.Backend == "" {
			continue
		}
		if e = add(p.Backend, p.Region); e != nil {
			return d, e
		}
		if p.Environment == "" {
			p.Environment = p.Stack
		}
		if p.RoleARN == "" {
			p.RoleARN = p.Values["AWS_ROLE_ARN"]
		}
		if p.Environment != "" && p.Environment != "shared" {
			if p.Repository != repo {
				return d, errors.New("local environment missing repository identity")
			}
			if e = addExtras(p.Environment, p.RoleARN, p.Region); e != nil {
				return d, e
			}
		}
	}
	names, err := nukeEnvironmentNames(g, repo)
	if err != nil {
		return d, err
	}
	for _, name := range names {
		snapshot, e := g.InspectEnvironment(repo, name)
		if e != nil || !snapshot.Exists {
			return d, errors.New("GitHub environment changed or is inaccessible")
		}
		v := snapshot.Variables
		if v["PULUMI_BACKEND_URL"] == "" && v["AWS_ROLE_ARN"] == "" {
			continue
		}
		if v["AWS_ACCOUNT_ID"] != account {
			return d, errors.New("GitHub environment AWS account mismatch")
		}
		if e = add(v["PULUMI_BACKEND_URL"], v["AWS_REGION"]); e != nil {
			return d, e
		}
		if e = addExtras(name, v["AWS_ROLE_ARN"], v["AWS_REGION"]); e != nil {
			return d, e
		}
	}
	// Expected owner and canonical ownership tags authorize a backend; bucket
	// names alone never authorize destructive recovery. Scan every owned bucket.
	var listing struct{ Buckets *[]struct{ Name string } }
	found, err := api("us-east-1").Call(&listing, "s3api", "list-buckets")
	if err != nil || !found || listing.Buckets == nil {
		return d, errors.New("state bucket discovery unavailable")
	}
	for _, bucket := range *listing.Buckets {

		region, owned, e := nukeOwnedBucket(api, account, bucket.Name)
		if e != nil {
			return d, e
		}
		if !owned {
			continue
		}
		if e = add("s3://"+bucket.Name, region); e != nil {
			return d, e
		}
	}
	keys := make([]string, 0, len(backends))
	for b := range backends {
		keys = append(keys, b)
	}
	sort.Strings(keys)
	for _, backend := range keys {
		region := backends[backend]
		u, _ := url.Parse(backend)
		// Explicitly evidenced backends still need ownership validation before being
		// promoted to recovery artifacts; unknown tags leave recovery unavailable.
		_, owned, e := nukeOwnedBucket(api, account, u.Host)
		if e != nil && !nukeAWSMissing(e, "NoSuchBucket") {
			return d, e
		}
		d.config.Projects = append(d.config.Projects, pulumicli.Project{Name: "attestra-application", Directory: filepath.Join(root, "infra"), Backend: backend, Region: region}, pulumicli.Project{Name: "attestra-control-plane", Directory: filepath.Join(root, "infra-index"), Backend: backend, Region: region, Shared: true})
		if owned {
			keys, keyErr := nukeBucketKeys(api(region), account, u.Host, region)
			if keyErr != nil {
				return d, keyErr
			}
			d.config.Recovery = append(d.config.Recovery, keys...)
			d.config.Recovery = append(d.config.Recovery, teardown.Artifact{ID: u.Host, Kind: "recovery_bucket", Region: region, Recovery: true, Data: map[string]string{"exclusive": "true"}})
			if strings.HasPrefix(u.Host, "attestra-control-plane-state-"+account+"-") {
				for _, key := range []string{"bootstrap-ready.json", "teardown-pending.json", "bootstrap.lock"} {
					d.artifacts = append(d.artifacts, teardown.Artifact{ID: u.Host + "/" + key, Kind: "attestra_marker", Region: region, Data: map[string]string{"bucket": u.Host, "key": key, "account": account, "project": "attestra-control-plane", "stack": "shared", "cleanup": "true"}})
				}
			}
		}
	}
	return d, nil
}
func nukeOwnedBucket(api func(string) awsenv.API, account, bucket string) (string, bool, error) {
	var location struct{ LocationConstraint *string }
	found, e := api("us-east-1").Call(&location, "s3api", "get-bucket-location", "--bucket", bucket, "--expected-bucket-owner", account)
	if e != nil {
		return "", false, e
	}
	if !found {
		return "", false, errors.New("bucket location unavailable")
	}
	region := "us-east-1"
	if location.LocationConstraint != nil && *location.LocationConstraint != "" {
		region = *location.LocationConstraint
	}
	if region == "EU" {
		region = "eu-west-1"
	}
	var tags struct {
		TagSet *[]struct{ Key, Value string }
	}
	found, e = api(region).Call(&tags, "s3api", "get-bucket-tagging", "--bucket", bucket, "--expected-bucket-owner", account)
	if nukeAWSMissing(e, "NoSuchTagSet") {
		return region, false, nil
	}
	if e != nil {
		return "", false, e
	}
	if !found || tags.TagSet == nil {
		return "", false, errors.New("bucket ownership unavailable")
	}
	app, environment, feature := "", "", ""
	for _, t := range *tags.TagSet {
		switch t.Key {
		case "ic:application":
			app = t.Value
		case "ic:environment":
			environment = t.Value
		case "ic:component":
			feature = t.Value
		}
	}
	return region, app == "attestra" && environment == "shared" && feature == "pulumi-state", nil
}

// Capture KMS identifiers without inferring exclusive ownership from usage by
// one bucket. A key shared with other applications remains visible and retained.
func nukeBucketKeys(a awsenv.API, account, bucket, region string) ([]teardown.Artifact, error) {
	var config struct {
		ServerSideEncryptionConfiguration *struct {
			Rules *[]struct {
				ApplyServerSideEncryptionByDefault struct{ SSEAlgorithm, KMSMasterKeyID string }
			}
		}
	}
	found, e := a.Call(&config, "s3api", "get-bucket-encryption", "--bucket", bucket, "--expected-bucket-owner", account)
	if nukeAWSMissing(e, "ServerSideEncryptionConfigurationNotFoundError") {
		return nil, nil
	}
	if e != nil || !found || config.ServerSideEncryptionConfiguration == nil || config.ServerSideEncryptionConfiguration.Rules == nil {
		return nil, errors.New("state bucket encryption metadata unavailable")
	}
	var out []teardown.Artifact
	for _, rule := range *config.ServerSideEncryptionConfiguration.Rules {
		value := rule.ApplyServerSideEncryptionByDefault
		if value.SSEAlgorithm == "AES256" {
			continue
		}
		if value.SSEAlgorithm != "aws:kms" && value.SSEAlgorithm != "aws:kms:dsse" {
			return nil, errors.New("unknown state bucket encryption")
		}
		if value.KMSMasterKeyID == "" {
			return nil, errors.New("state bucket KMS identity unavailable")
		}
		key, e := nukeKeyEvidence(a, account, region, value.KMSMasterKeyID)
		if e != nil {
			return nil, e
		}
		if key.ID != "" {
			out = append(out, key)
		}
	}
	return out, nil
}
func nukeKeyEvidence(a awsenv.API, account, region, id string) (teardown.Artifact, error) {
	var metadata struct {
		KeyMetadata *struct{ Arn, AWSAccountId, KeyManager string }
	}
	found, e := a.Call(&metadata, "kms", "describe-key", "--key-id", id)
	if e != nil || !found || metadata.KeyMetadata == nil {
		return teardown.Artifact{}, errors.New("state encryption key metadata unavailable")
	}
	m := metadata.KeyMetadata
	parts := strings.Split(m.Arn, ":")
	if len(parts) != 6 || parts[2] != "kms" || parts[3] != region || parts[4] != account || m.AWSAccountId != account {
		return teardown.Artifact{}, errors.New("state encryption key account mismatch")
	}
	if m.KeyManager == "AWS" {
		return teardown.Artifact{}, nil
	}
	if m.KeyManager != "CUSTOMER" {
		return teardown.Artifact{}, errors.New("state encryption key ownership unknown")
	}
	// An app tag does not establish that no other service relies on this key.
	return teardown.Artifact{ID: m.Arn, Kind: "recovery_key", Region: region, Recovery: true, Data: map[string]string{"exclusive": "false", "reason": "KMS exclusivity requires explicit independent evidence"}}, nil
}
