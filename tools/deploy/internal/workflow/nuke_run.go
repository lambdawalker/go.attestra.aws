package workflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/term"
	awsenv "github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/aws"
	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/dns"
	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/github"
	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/ui"
	"github.com/lambdawalker/go.infra.composer/teardown"
	"github.com/lambdawalker/go.infra.composer/teardown/pulumicli"
)

func validateComposerNukeFlags(nuke, teardown bool, supplied []string) error {
	for _, f := range supplied {
		if (f == "check" || f == "fast-check") && (!nuke || !teardown) {
			return fmt.Errorf("-%s requires -teardown -nuke", f)
		}
	}
	if !nuke {
		return nil
	}
	if !teardown {
		return errors.New("-nuke requires -teardown")
	}
	for _, f := range supplied {
		switch f {
		case "teardown", "nuke", "check", "fast-check", "repo-root":
		default:
			return fmt.Errorf("-%s cannot be combined with -teardown -nuke", f)
		}
	}
	return nil
}

func ensureNoActiveComposerNuke(root string) error {
	locks, err := filepath.Glob(filepath.Join(root, ".attestra", "nuke.*.lock"))
	if err != nil {
		return err
	}
	if len(locks) > 0 {
		return errors.New("nuke is active or interrupted; inspect its report and lock before running another operator flow")
	}
	return nil
}

func mergeSavedNukeProjects(root string, cfg *pulumicli.Config, r *teardown.Report) error {
	if r.Scope != cfg.Scope || r.Version != 1 {
		return errors.New("nuke report application/account/version mismatch")
	}
	for _, t := range r.Targets {
		dir, shared := "", false
		switch t.Project {
		case "attestra-application":
			dir = filepath.Join(root, "infra")
		case "attestra-control-plane":
			dir = filepath.Join(root, "infra-index")
			shared = true
		default:
			return errors.New("nuke report contains an unrelated project")
		}
		if t.Shared != shared || (t.Directory != "" && t.Directory != dir) {
			return errors.New("saved target phase or checkout directory changed; inspect the nuke report")
		}
		found := false
		for _, p := range cfg.Projects {
			if p.Name == t.Project && p.Backend == t.Backend {
				if p.Region != t.Region || p.Shared != shared {
					return errors.New("saved project identity changed")
				}
				found = true
			}
		}
		if !found {
			cfg.Projects = append(cfg.Projects, pulumicli.Project{Name: t.Project, Directory: dir, Backend: t.Backend, Region: t.Region, Shared: shared})
		}
	}
	for _, a := range r.Artifacts {
		if !a.Recovery {
			continue
		}
		found := false
		for _, b := range cfg.Recovery {
			if a.Key() == b.Key() {
				found = true
			}
		}
		if !found {
			cfg.Recovery = append(cfg.Recovery, a)
		}
		// Artifact-only reports can survive after every stack has disappeared.
		// Reconstruct provider routing from a previously verified state bucket;
		// this does not list or create any new environment target.
		if a.Kind == "recovery_bucket" {
			if !regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`).MatchString(a.ID) || a.Region == "" {
				return errors.New("invalid saved recovery bucket")
			}
			for _, p := range []pulumicli.Project{{Name: "attestra-application", Directory: filepath.Join(root, "infra"), Backend: "s3://" + a.ID, Region: a.Region}, {Name: "attestra-control-plane", Directory: filepath.Join(root, "infra-index"), Backend: "s3://" + a.ID, Region: a.Region, Shared: true}} {
				present := false
				for _, old := range cfg.Projects {
					if old.Name == p.Name && old.Backend == p.Backend {
						present = true
					}
				}
				if !present {
					cfg.Projects = append(cfg.Projects, p)
				}
			}
		}
	}
	return nil
}

// The child CLIs inherit the existing isolated, renewable administrator session.
// Per-backend passphrases are memory-only and never reach the report.
type composerNukeRunner struct {
	env    []string
	passes map[string]string
}

func (r *composerNukeRunner) Run(ctx context.Context, dir string, overlay []string, name string, args ...string) ([]byte, error) {
	env := replaceEnvironment(r.env, environmentMap(overlay))
	if name == "pulumi" {
		backend := environmentMap(overlay)["PULUMI_BACKEND_URL"]
		if pass, ok := r.passes[backend+"|"+dir]; ok {
			env = replaceEnvironment(env, map[string]string{"PULUMI_CONFIG_PASSPHRASE": pass})
		}
	}
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	c.Env = env
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	if err != nil {
		return append(stdout.Bytes(), stderr.Bytes()...), err
	}
	return stdout.Bytes(), nil
}
func environmentMap(env []string) map[string]string {
	m := map[string]string{}
	for _, v := range env {
		if k, x, ok := strings.Cut(v, "="); ok {
			m[k] = x
		}
	}
	return m
}

func runComposerNuke(root string, check, fast bool) error {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return errors.New("nuke and nuke checks require an interactive terminal")
	}
	if err := ensureNoActiveComposerNuke(root); err != nil {
		return err
	}
	if !check {
		if err := (&bootstrapWizard{root: root}).preflight(); err != nil {
			return err
		}
	}
	repo := localRepository(root)
	if repo == "" {
		repo = "lambdawalker/go.attestra.aws"
	}
	if err := ui.Input("GitHub repository", &repo, false, true).Run(); err != nil {
		return err
	}
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(repo, " ?#\r\n") {
		return errors.New("use owner/repository")
	}
	vaultName := ""
	if err := ui.Input("Optional credential vault (credentials only; nuke targets ALL environments)", &vaultName, false, false).Run(); err != nil {
		return err
	}
	w := &bootstrapWizard{root: root, environment: vaultName}
	defer w.cleanup()
	if vaultName != "" {
		if err := ui.ValidateEnvironment(vaultName); err != nil {
			return err
		}
		if err := w.loadCredentials(repo, false); err != nil {
			return err
		}
	}
	token := w.secrets.GitHub
	if token == "" {
		if err := ui.Input("GitHub token (read for checks; Environments write for nuke)", &token, true, !check).Run(); err != nil {
			return err
		}
	}
	var g *github.Client
	var metadata repositoryMetadata
	if strings.TrimSpace(token) != "" {
		g = github.NewClient(strings.TrimSpace(token))
		found, err := g.Request("GET", "/repos/"+repo, nil, &metadata)
		if err != nil {
			return err
		}
		if !found {
			return errors.New("repository unavailable; check token access")
		}
	}
	if !check {
		if err := ensureNoComposerNukeDeployments(g, repo); err != nil {
			return err
		}
	}
	o := options{Root: root, Stack: "nuke", Region: "us-east-2", Profile: "attestra", Backend: "s3://attestra-nuke-discovery"}
	mode := "sso"
	if err := huh.NewSelect[string]().Title("AWS administrator authentication").Options(huh.NewOption("SSO", "sso"), huh.NewOption("AWS browser login", "login"), huh.NewOption("Access key credentials", "keys")).Value(&mode).Run(); err != nil {
		return err
	}
	c := credentials{Access: w.secrets.AWSAccess, Secret: w.secrets.AWSSecret, Token: w.secrets.AWSToken}
	if mode == "keys" {
		if c.Access == "" || c.Secret == "" {
			if err := huh.NewForm(huh.NewGroup(ui.Input("AWS access key ID", &c.Access, true, true), ui.Input("AWS secret access key", &c.Secret, true, true), ui.Input("AWS session token", &c.Token, true, false))).Run(); err != nil {
				return err
			}
		}
	} else {
		if err := ui.Input("AWS administrator profile", &o.Profile, false, true).Run(); err != nil {
			return err
		}
		o.Sso, o.Login = mode == "sso", mode == "login"
		var err error
		c, err = loginCredentials(&processRunner{env: loginEnvironment(os.Environ(), o.Region)}, o)
		if err != nil {
			return err
		}
	}
	tmp, err := os.MkdirTemp("", "attestra-composer-nuke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	empty := filepath.Join(tmp, "aws-config")
	if err = os.WriteFile(empty, nil, 0600); err != nil {
		return err
	}
	env, err := prepareCloudEnvironment(os.Environ(), c, o, empty)
	if err != nil {
		return err
	}
	a := &awsenv.Client{Env: env, RenewalFailure: credentialRenewalFailure}
	var identity struct{ Account, Arn string }
	if found, e := a.Call(&identity, "sts", "get-caller-identity"); e != nil {
		return e
	} else if !found || len(identity.Account) != 12 {
		return errors.New("invalid AWS account identity")
	}
	scope := teardown.Scope{Application: "attestra", Account: identity.Account}
	path := filepath.Join(root, ".attestra", "nuke."+identity.Account+".local.json")
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("nuke report is locked; inspect the lock before retrying")
	}
	fmt.Fprintf(lock, "pid %d\n", os.Getpid())
	lock.Close()
	defer os.Remove(path + ".lock")
	store := teardown.FileStore{Path: path}
	previous, err := store.Load()
	if errors.Is(err, fs.ErrNotExist) && !check {
		previous = &teardown.Report{Version: 1, Scope: scope}
	} else if err != nil {
		return errors.New("cannot load prior Composer nuke report")
	}
	if previous.Scope != scope || previous.Version != 1 {
		return errors.New("nuke report identity/schema mismatch; do not substitute a legacy report")
	}
	cfg := pulumicli.Config{Scope: scope}
	if !check {
		cfg, err = discoverNukeConfig(root, repo, identity.Account, env, g)
		if err != nil {
			return err
		}
	}
	if err = mergeSavedNukeProjects(root, &cfg, previous); err != nil {
		return err
	}
	if len(cfg.Projects) == 0 && !check {
		return errors.New("no verified application projects found; no teardown authorized")
	}
	runner := &composerNukeRunner{env: env, passes: map[string]string{}}
	var base *pulumicli.Driver
	if len(cfg.Projects) > 0 {
		base, err = pulumicli.New(cfg, runner)
		if err != nil {
			return err
		}
	}
	d := &composerNukeDriver{Base: base, Root: root, Repository: repo, Account: identity.Account, CallerARN: identity.Arn, Env: env, GitHub: g, Metadata: metadata}
	cfToken := w.secrets.Cloudflare
	if err = ui.Input("Cloudflare token (blank leaves required DNS cleanup unverifiable)", &cfToken, true, false).Run(); err != nil {
		return err
	}
	if strings.TrimSpace(cfToken) != "" {
		d.Cloudflare = dns.NewClient(strings.TrimSpace(cfToken))
	}
	engine := teardown.Engine{Scope: scope, Driver: d, Store: store, Recovery: cfg.Recovery}
	if !check {
		d.BeforeMutation = func() error { return ensureNoComposerNukeDeployments(g, repo) }
		consent := map[string]bool{}
		engine.Store = approvedNukeStore{FileStore: store, Recovery: func() []teardown.Artifact { return cfg.Recovery }}
		if err = d.DiscoverArtifacts(context.Background()); err != nil {
			return err
		}
		fmt.Printf("Repository: %s\nAccount: %s\nIdentity: %s\n", repo, identity.Account, identity.Arn)
		for _, p := range cfg.Projects {
			fmt.Printf("ALL STACKS %s • %s • %s • shared=%t\n", p.Name, p.Backend, p.Region, p.Shared)
		}
		for _, a := range cfg.Recovery {
			fmt.Printf("RECOVERY LAST %s • %s • %s\n", a.Kind, a.ID, a.Region)
		}
		fmt.Println("Stop all deployments. This permanently removes application data. Missing artifacts are success; unknown or remaining artifacts retain recovery state.")
		if err = typedConfirmation("Authorize all environments and shared admin/index", "nuke "+repo+" "+identity.Account); err != nil {
			return err
		}
		if err = approveComposerNukeKeys(&cfg, consent); err != nil {
			return err
		}
		base.Config.Recovery = cfg.Recovery
		engine.Recovery = cfg.Recovery
		previous.Artifacts = mergeComposerNukeArtifacts(previous.Artifacts, d.AdditionalArtifacts())
		if err = store.Save(previous); err != nil {
			return err
		}
		d.BeforeInventory = func(t teardown.Target) error {
			key := t.Backend + "|" + t.Directory
			if _, ok := runner.passes[key]; !ok {
				pass := w.secrets.Pulumi
				if t.Shared {
					pass = w.secrets.IndexPulumi
				}
				if err := ui.Input("Existing Pulumi passphrase for "+t.Project+" at "+t.Backend+" (blank for KMS)", &pass, true, false).Run(); err != nil {
					return err
				}
				runner.passes[key] = pass
			}
			// Read encrypted export metadata only; never decrypt or persist values.
			// Missing stacks/backends are classified authoritatively by Base.Inventory.
			b, exportErr := runner.Run(context.Background(), t.Directory, []string{"PULUMI_BACKEND_URL=" + t.Backend, "AWS_REGION=" + t.Region, "AWS_DEFAULT_REGION=" + t.Region}, "pulumi", "stack", "export", "--stack", t.Stack, "--non-interactive")
			if exportErr != nil {
				return nil
			}
			keyEvidence, keyErr := nukePulumiKey(b, t.Region)
			if keyErr != nil {
				return keyErr
			}
			if keyEvidence.ID != "" {
				keyEvidence, keyErr = nukeKeyEvidence(d.api(keyEvidence.Region), identity.Account, keyEvidence.Region, keyEvidence.ID)
				if keyErr != nil {
					return keyErr
				}
				if keyEvidence.ID != "" {
					found := false
					for _, old := range cfg.Recovery {
						if old.Kind == "recovery_key" && old.ID == keyEvidence.ID && old.Region == keyEvidence.Region {
							found = true
						}
					}
					if !found {
						cfg.Recovery = append(cfg.Recovery, keyEvidence)
					}
					if keyErr = approveComposerNukeKeys(&cfg, consent); keyErr != nil {
						return keyErr
					}
					base.Config.Recovery = cfg.Recovery
					engine.Recovery = cfg.Recovery
				}
			}
			return nil
		}
		if err = ensureNoComposerNukeDeployments(g, repo); err != nil {
			return err
		}
	}
	var report *teardown.Report
	if check {
		report, err = engine.Check(context.Background(), fast)
	} else {
		report, err = engine.Nuke(context.Background())
	}
	if report != nil {
		printComposerNukeReport(report, check)
		fmt.Println("Persistent report:", path)
	}
	return err
}

func approveComposerNukeKeys(cfg *pulumicli.Config, consent map[string]bool) error {
	for i := range cfg.Recovery {
		key := &cfg.Recovery[i]
		if key.Kind != "recovery_key" {
			continue
		}
		identity := key.Region + "|" + key.ID
		if key.Data["exclusive"] == "true" {
			consent[identity] = true
			continue
		}
		exclusive, known := consent[identity]
		if !known {
			if err := huh.NewConfirm().Title("Is this KMS key exclusive to Attestra: " + key.ID + "?").Description("Keep No if shared or uncertain; recovery state will be retained.").Value(&exclusive).Run(); err != nil {
				return err
			}
			if exclusive {
				if err := typedConfirmation("Confirm independent evidence that this key is exclusive", "delete key "+key.ID); err != nil {
					return err
				}
			}
			consent[identity] = exclusive
		}
		if exclusive {
			if key.Data == nil {
				key.Data = map[string]string{}
			}
			key.Data["exclusive"] = "true"
		}
	}
	return nil
}

// A later explicit ownership approval supersedes the old ownership blocker,
// while the real key remains inventoried in the gated recovery phase.
type approvedNukeStore struct {
	teardown.FileStore
	Recovery func() []teardown.Artifact
}

func (s approvedNukeStore) Save(r *teardown.Report) error {
	approved := map[string]bool{}
	for _, a := range s.Recovery() {
		if a.Kind == "recovery_key" && a.Recovery && a.Data["exclusive"] == "true" {
			approved[a.Region+"|"+a.ID] = true
		}
	}
	filtered := r.Artifacts[:0]
	for _, a := range r.Artifacts {
		if a.Kind == "attestra_retained_key" && approved[a.Region+"|"+a.ID] {
			continue
		}
		filtered = append(filtered, a)
	}
	r.Artifacts = filtered
	return s.FileStore.Save(r)
}

func mergeComposerNukeArtifacts(old, more []teardown.Artifact) []teardown.Artifact {
	seen := map[string]bool{}
	out := append([]teardown.Artifact{}, old...)
	for _, a := range old {
		seen[a.Key()] = true
	}
	for _, a := range more {
		if !seen[a.Key()] {
			out = append(out, a)
			seen[a.Key()] = true
		}
	}
	return out
}

func ensureNoComposerNukeDeployments(g *github.Client, repo string) error {
	if g == nil {
		return errors.New("GitHub deployment discovery unavailable")
	}
	for _, status := range []string{"queued", "in_progress", "waiting", "pending", "requested"} {
		for page := 1; ; page++ {
			var body struct {
				WorkflowRuns *[]struct{ Path string } `json:"workflow_runs"`
			}
			found, err := g.Request("GET", fmt.Sprintf("/repos/%s/actions/runs?per_page=100&page=%d&status=%s", repo, page, status), nil, &body)
			if err != nil || !found || body.WorkflowRuns == nil {
				return errors.New("cannot verify absence of active deployments")
			}
			for _, run := range *body.WorkflowRuns {
				if run.Path == "" {
					return errors.New("incomplete workflow listing")
				}
				path := strings.SplitN(run.Path, "@", 2)[0]
				if path == ".github/workflows/deploy.yml" || path == ".github/workflows/deploy.yaml" {
					return errors.New("deployment workflow is active; stop it before nuke")
				}
			}
			if len(*body.WorkflowRuns) < 100 {
				break
			}
		}
	}
	return nil
}

func printComposerNukeReport(r *teardown.Report, check bool) {
	results := r.Results
	if check {
		results = r.Checks
		fmt.Println("Check scope:", r.CheckScope, "(per-artifact timestamps distinguish cached evidence)")
	}
	envs := map[string]string{}
	for _, t := range r.Targets {
		envs[t.ID] = t.Environment
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "STATUS\tARTIFACT TYPE\tZONE/REGION\tENVIRONMENT\tARTIFACT\tDETAIL")
	for _, v := range results {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", v.Status, v.Artifact.Kind, v.Artifact.Region, envs[v.Artifact.TargetID], v.Artifact.ID, v.Detail)
	}
	w.Flush()
}
