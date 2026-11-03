package workflow

// Reports deliberately contain safe identities only. Provider diagnostics, raw
// exports, event logs and credential values never enter this model.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/dns"
	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/github"
	"github.com/lambdawalker/go.infra.composer/reporting"
	reportcli "github.com/lambdawalker/go.infra.composer/reporting/pulumicli"
)

var errNoArtifactCache = errors.New("no matching cached artifact report; deploy or select the correct scope")

type artifactReportFlags struct {
	Report, Refresh, Preview             bool
	Scope, Account, Feature, Environment string
}

func containsReportFlag(flags []string, s string) bool {
	for _, f := range flags {
		if f == s {
			return true
		}
	}
	return false
}
func validateArtifactFlags(f artifactReportFlags, flags []string) error {
	if f.Refresh && !f.Report {
		return errors.New("-refresh requires -report")
	}
	if f.Report && f.Preview {
		return errors.New("choose -report or -preview")
	}
	if !f.Report && !f.Preview {
		for _, name := range []string{"report", "preview", "refresh", "env", "account", "feature"} {
			if containsReportFlag(flags, name) {
				return errors.New("artifact reporting flags require -report or -preview")
			}
		}
		return nil
	}
	if f.Scope != "app" && f.Scope != "shared" && f.Scope != "both" {
		return errors.New("-scope must be app, shared, or both")
	}
	allowed := map[string]bool{"report": true, "preview": true, "refresh": true, "scope": true, "stack": true, "env": true, "account": true, "feature": true, "repo-root": true, "backend": true, "region": true}
	for _, s := range flags {
		if !allowed[s] {
			return fmt.Errorf("-%s cannot be combined with artifact reporting modes", s)
		}
	}
	return nil
}
func artifactCacheDir(root string) string { return filepath.Join(root, ".cache", "infra-composer") }
func artifactBackend(raw string) string {
	u, e := url.Parse(raw)
	if e != nil {
		return ""
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	return u.String()
}
func artifactProject(dir string) string {
	if filepath.Base(dir) == "infra-index" {
		return "attestra-control-plane"
	}
	return "attestra-application"
}
func artifactCheckpoint(root, stack string) bootstrapCheckpoint {
	var c bootstrapCheckpoint
	b, e := os.ReadFile(filepath.Join(root, ".attestra", "bootstrap."+stack+".local.json"))
	if e == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}
func reportEnv(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], key+"=") {
			return strings.TrimPrefix(env[i], key+"=")
		}
	}
	return ""
}
func artifactScope(p *processRunner, root, dir, stack string) reporting.Scope {
	c := artifactCheckpoint(root, stack)
	repo := c.Repository
	if repo == "" {
		repo = localRepository(root)
	}
	if repo == "" {
		repo = "lambdawalker/go.attestra.aws"
	}
	env := p.env
	if env == nil {
		env = os.Environ()
	}
	backend := artifactBackend(reportEnv(env, "PULUMI_BACKEND_URL"))
	if backend == "" {
		backend = artifactBackend(c.Backend)
	}
	account := p.reportAccount
	if account == "" {
		account = reportEnv(env, "AWS_ACCOUNT_ID")
	}
	if account == "" {
		account = c.Account
	}
	region := reportEnv(env, "AWS_REGION")
	if region == "" {
		region = reportEnv(env, "AWS_DEFAULT_REGION")
	}
	if region == "" {
		region = c.Region
	}
	return reporting.Scope{Application: "attestra", Repository: repo, Environment: stack, Account: account, Region: region, Backend: backend, Project: artifactProject(dir), Stack: stack}
}
func loadArtifactScopes(root string, o options, f artifactReportFlags) ([]reporting.Report, error) {
	paths, e := filepath.Glob(filepath.Join(artifactCacheDir(root), "*.json"))
	if e != nil {
		return nil, e
	}
	var out []reporting.Report
	seen := map[string]bool{}
	for _, path := range paths {
		b, e := os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		var raw reporting.Report
		if json.Unmarshal(b, &raw) != nil {
			return nil, errors.New("invalid artifact cache")
		}
		s := raw.Scope
		if s.Application != "attestra" {
			continue
		}
		if s.Account == "" || s.Backend == "" || s.Region == "" || s.Project == "" || s.Stack == "" {
			continue
		}
		if repo := localRepository(root); repo != "" && s.Repository != repo {
			continue
		}
		if s.Project != "attestra-application" && s.Project != "attestra-control-plane" {
			continue
		}
		shared := s.Project == "attestra-control-plane"
		if (f.Scope == "app" && shared) || (f.Scope == "shared" && !shared) {
			continue
		}
		if !shared && o.Stack != "" && s.Stack != o.Stack {
			continue
		}
		if shared && s.Stack != "shared" {
			continue
		}
		if f.Account != "" && s.Account != f.Account {
			continue
		}
		if o.Backend != "" && s.Backend != artifactBackend(o.Backend) {
			continue
		}
		if o.Region != "" && s.Region != o.Region {
			continue
		}
		key := s.Project
		if seen[key] {
			return nil, errors.New("ambiguous artifact cache scope; select -stack, -account, -region and -backend")
		}
		seen[key] = true
		r, e := reporting.LoadCache(path, s)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, errNoArtifactCache
	}
	if f.Scope == "both" && len(out) != 2 {
		return nil, errors.New("both requires cached application and shared scopes")
	}
	return out, nil
}
func artifactOutput(root, label string) string {
	return filepath.Join(root, "deployment-reports", time.Now().UTC().Format("20060102T150405.000000000Z")+"-"+label)
}
func writeArtifactBundle(dir string, r reporting.Report) error {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	writers := []struct {
		ext   string
		write func(io.Writer, reporting.Report) error
	}{{"md", reporting.WriteMarkdown}, {"json", reporting.WriteJSON}, {"html", reporting.WriteHTML}}
	for _, w := range writers {
		f, e := os.OpenFile(filepath.Join(dir, "artifacts."+w.ext), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		e = w.write(f, r)
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
	}
	return nil
}
func runArtifactReports(o options, f artifactReportFlags, out io.Writer) error {
	reports, e := loadArtifactScopes(o.Root, o, f)
	// Native preview can operate without an earlier snapshot when an explicit backend is supplied.
	if errors.Is(e, errNoArtifactCache) && f.Preview && o.Backend != "" && f.Scope != "both" {
		dir := filepath.Join(o.Root, "infra")
		stack := o.Stack
		if f.Scope == "shared" {
			dir = filepath.Join(o.Root, "infra-index")
			stack = "shared"
		}
		p := &processRunner{env: os.Environ()}
		s := artifactScope(p, o.Root, dir, stack)
		s.Backend = artifactBackend(o.Backend)
		if o.Region != "" {
			s.Region = o.Region
		}
		s.Account = f.Account
		reports = []reporting.Report{{Scope: s}}
		e = nil
	}
	if e != nil {
		return e
	}
	for _, cached := range reports {
		reportDir := ""
		r := cached
		if f.Refresh || f.Preview {
			s := r.Scope
			if e := (options{Backend: s.Backend, Stack: s.Stack, Region: s.Region}).validate(); e != nil {
				return errors.New("read-only reporting requires a valid backend, region and stack")
			}
			dir := filepath.Join(o.Root, "infra")
			if s.Project == "attestra-control-plane" {
				dir = filepath.Join(o.Root, "infra-index")
			}
			env := replaceEnvironment(os.Environ(), map[string]string{"PULUMI_BACKEND_URL": s.Backend + "?region=" + url.QueryEscape(s.Region) + "&awssdk=v2", "AWS_REGION": s.Region, "AWS_DEFAULT_REGION": s.Region, "PULUMI_DEBUG_COMMANDS": "true"})
			// Credentials are supplied by the normal AWS chain and Pulumi environment;
			// reporting never prompts for supplemental tokens.
			p := &processRunner{env: env, reportDisabled: true, reportAccount: s.Account}
			id, e := p.Exec(o.Root, true, "aws", "sts", "get-caller-identity", "--output", "json", "--no-cli-pager")
			var identity struct{ Account string }
			if e != nil || json.Unmarshal(id, &identity) != nil || identity.Account == "" || (s.Account != "" && identity.Account != s.Account) {
				return errors.New("artifact reporting AWS identity does not match selected scope")
			}
			s.Account = identity.Account
			projectFile, e := os.ReadFile(filepath.Join(dir, "Pulumi.yaml"))
			if e != nil || !strings.Contains(strings.ReplaceAll(string(projectFile), "\r\n", "\n"), "name: "+s.Project+"\n") {
				return errors.New("artifact reporting project does not match selected scope")
			}
			operation := "refresh"
			if f.Preview {
				operation = "preview"
			}
			a := beginArtifactAttempt(p, p, o.Root, dir, s.Stack, operation)
			a.report.Scope = s
			a.cached = cached
			reportDir = a.output
			if f.Preview {
				a.report.Metadata.Operation = "preview"
				path, cleanup, e := previewEventLog()
				if e != nil {
					return e
				}
				_, runErr := p.Exec(dir, false, "pulumi", "preview", "--stack", s.Stack, "--non-interactive", "--event-log", path)
				a.previewFile(path)
				cleanup()
				a.finish(runErr)
				if runErr != nil {
					return errors.New("native preview failed; partial artifact report saved")
				}
			} else {
				a.upAttempted = true
				a.finish(nil)
			}
			r = a.report
		}
		r.Artifacts = reporting.SelectFeature(r.Artifacts, f.Feature)
		if reportDir == "" {
			reportDir = artifactOutput(o.Root, "cached")
			fmt.Fprintln(os.Stderr, "Artifact reports:", reportDir)
		}
		if e := writeArtifactBundle(reportDir, r); e != nil {
			return e
		}
		if e := reporting.WriteMarkdown(out, r); e != nil {
			return e
		}
	}
	return nil
}

type artifactAttempt struct {
	knownDNS                 map[string]reporting.Artifact
	nativeChecked            bool
	state                    []byte
	p                        *processRunner
	r                        commandRunner
	root, dir, stack, output string
	report, cached           reporting.Report
	upAttempted              bool
	upSucceeded              bool
}

func beginArtifactAttempt(p *processRunner, r commandRunner, root, dir, stack, operation string) *artifactAttempt {
	if p.reportRunner != nil {
		r = p.reportRunner
	}
	a := &artifactAttempt{p: p, r: r, root: root, dir: dir, stack: stack, output: artifactOutput(root, filepath.Base(dir))}
	a.report = reporting.Report{Scope: artifactScope(p, root, dir, stack), Metadata: reporting.Metadata{Operation: operation, Outcome: "in_progress", StartedAt: time.Now().UTC()}}
	if b, e := r.Exec(root, true, "git", "rev-parse", "HEAD"); e == nil {
		a.report.Metadata.Revision = strings.TrimSpace(string(b))
	}
	if b, e := r.Exec(root, true, "git", "status", "--porcelain"); e == nil {
		a.report.Metadata.Dirty = len(bytes.TrimSpace(b)) > 0
	}
	if cached, e := reporting.LoadCache(reporting.CachePath(artifactCacheDir(root), a.report.Scope), a.report.Scope); e == nil {
		a.cached = cached
		a.report.CacheLoaded = true
	}
	a.rememberDNSArtifacts(a.cached.Artifacts)
	a.readState(false)
	a.persist()
	return a
}
func (a *artifactAttempt) warn(code string) {
	for _, s := range a.report.Warnings {
		if s == code {
			return
		}
	}
	a.report.Warnings = append(a.report.Warnings, code)
}
func (a *artifactAttempt) readState(after bool) []byte {
	b, e := a.r.Exec(a.dir, true, "pulumi", "stack", "export", "--stack", a.stack)
	if e != nil {
		a.warn("state_read_failed")
		a.retainInventory()
		return nil
	}
	as, e := reportcli.ReadState(bytes.NewReader(b), time.Now().UTC())
	if e != nil {
		a.warn("state_invalid")
		a.retainInventory()
		return nil
	}
	a.rememberDNSState(b)
	as = overlayArtifactOwnership(as, b, a.report.Scope.Repository)
	old := map[string]reporting.Artifact{}
	for _, x := range a.report.Artifacts {
		old[x.Key] = x
	}
	for i := range as {
		if as[i].Owner == "" {
			as[i].Owner = a.report.Scope.Repository
		}
		if x, ok := old[as[i].Key]; ok {
			as[i].PlannedAction = x.PlannedAction
			if after && x.ProviderID != "" && x.ProviderID != as[i].ProviderID {
				prior := x
				prior.Key = x.Key + "#previous-physical-id:" + x.ProviderID
				prior.PlannedAction = "previous physical identity (replacement)"
				prior.Observation = reporting.Observation{Status: reporting.Unknown, Reason: reporting.IdentityChanged}
				as = append(as, prior)
			}
		}
	}
	if after {
		for _, x := range a.report.Artifacts {
			found := false
			for _, n := range as {
				if x.Key == n.Key {
					found = true
					break
				}
			}
			if !found && x.Provider == "aws" {
				x.PlannedAction = "previous physical identity (removed from state)"
				x.Observation = reporting.Observation{Status: reporting.Unknown, Reason: reporting.Unchecked}
				as = append(as, x)
			}
		}
	}
	if !after {
		known := map[string]bool{}
		for _, x := range as {
			known[x.Key] = true
		}
		for _, x := range a.cached.Artifacts {
			if !known[x.Key] {
				x.Observation = reporting.Observation{Status: reporting.Unknown, Reason: reporting.Unchecked}
				as = append(as, x)
			}
		}
	}
	a.report.Artifacts = as
	return b
}
func (a *artifactAttempt) retainInventory() {
	if len(a.report.Artifacts) == 0 {
		a.report.Artifacts = append([]reporting.Artifact(nil), a.cached.Artifacts...)
	}
	for i := range a.report.Artifacts {
		a.report.Artifacts[i].Observation = reporting.Observation{Status: reporting.Unknown, Reason: reporting.CheckFailed, ObservedAt: time.Now().UTC(), Source: "pulumi-state"}
	}
}
func (a *artifactAttempt) previewFile(path string) {
	f, e := os.Open(path)
	if e != nil {
		a.warn("preview_events_unavailable")
		return
	}
	defer f.Close()
	as, e := reportcli.ApplyPreview(a.report.Artifacts, f)
	a.report.Artifacts = overlayArtifactOwnership(as, nil, a.report.Scope.Repository)
	if e != nil {
		a.warn("preview_events_invalid")
	}
}

// reportRefreshRunner is explicitly read-only and cannot recursively trigger hooks.
type reportRefreshRunner struct{ r commandRunner }

func (r reportRefreshRunner) Run(ctx context.Context, dir string, args []string, out io.Writer) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	_, e := r.r.Exec(dir, true, "pulumi", args...)
	return e
}
func (a *artifactAttempt) finish(runErr error) {
	if a.upAttempted || a.report.Metadata.Operation != "preview" {
		if !a.nativeChecked {
			a.checkNative()
		}
		a.supplemental(a.state)
	} else {
		// Reuse live evidence only when scope + physical identity match, preserving time.
		for i, x := range a.report.Artifacts {
			for _, old := range a.cached.Artifacts {
				if a.report.Scope == a.cached.Scope && x.Key == old.Key && x.Provider == old.Provider && x.ProviderID != "" && x.ProviderID == old.ProviderID {
					a.report.Artifacts[i].Observation = old.Observation
					a.report.CacheLoaded = true
					break
				}
			}
		}
	}
	a.report.Metadata.FinishedAt = time.Now().UTC()
	a.report.Metadata.Outcome = "succeeded"
	if runErr != nil {
		a.report.Metadata.Outcome = "failed"
		if a.upSucceeded {
			a.report.Metadata.Outcome = "infrastructure_succeeded_followup_failed"
		}
	}
	a.persist()
	a.p.lastReport = a
	fmt.Fprintln(os.Stderr, "Artifact reports:", a.output)
}
func (a *artifactAttempt) persist() {
	a.report = reporting.Normalize(a.report)
	if e := writeArtifactBundle(a.output, a.report); e != nil {
		fmt.Fprintln(os.Stderr, "Artifact report write failed")
	}
	if a.report.Metadata.Operation != "preview" && a.report.Scope.Account != "" && a.report.Scope.Backend != "" && a.report.Scope.Region != "" {
		if e := reporting.SaveCache(reporting.CachePath(artifactCacheDir(a.root), a.report.Scope), a.report); e != nil {
			fmt.Fprintln(os.Stderr, "Artifact cache write failed")
		}
	}
}
func (a *artifactAttempt) supplemental(state []byte) {
	a.rememberDNSArtifacts(a.report.Artifacts)
	native := a.report.Artifacts[:0]
	for _, x := range a.report.Artifacts {
		if !strings.HasPrefix(x.Key, "external:") {
			native = append(native, x)
		}
	}
	a.report.Artifacts = native
	s := a.report.Scope
	now := time.Now().UTC()
	unknown := func(source string) reporting.Observation {
		return reporting.Observation{Status: reporting.Unknown, Reason: reporting.CheckFailed, Source: source, ObservedAt: now}
	}
	add := func(key, name, typ, provider, id string) reporting.Artifact {
		return reporting.Artifact{Key: key, Name: name, Type: typ, Provider: provider, ProviderID: id, Owner: s.Repository, Feature: "attestra-deployment", Priority: 40, Observation: unknown(provider + "-api")}
	}
	env := a.p.env
	if env == nil {
		env = os.Environ()
	}
	gh := a.p.reportGitHub
	if gh == nil {
		token := reportEnv(env, "GH_TOKEN")
		if token == "" {
			token = reportEnv(env, "GITHUB_TOKEN")
		}
		if token != "" {
			gh = github.NewClient(token)
		}
	}
	if gh == nil {
		a.warn("github_credentials_unavailable")
	}
	if cfToken := reportEnv(env, "CLOUDFLARE_API_TOKEN"); a.p.reportDNS == nil && cfToken == "" {
		a.warn("cloudflare_credentials_unavailable")
	}
	if s.Project == "attestra-application" {
		x := add("external:github:"+s.Repository+":"+s.Environment, s.Environment, "github:environment", "github", s.Repository+"/"+s.Environment)
		if gh == nil {
			x.Observation = reporting.Observation{Status: reporting.Unknown, Reason: reporting.Unchecked, Source: "github-auth-unavailable"}
		}
		if gh != nil {
			names, e := nukeEnvironmentNames(gh, s.Repository)
			if e == nil {
				x.Observation.Status = reporting.Absent
				x.Observation.Reason = reporting.NotFound
				for _, n := range names {
					if n == s.Environment {
						x.Observation.Status = reporting.Present
						x.Observation.Reason = ""
					}
				}
			}
		}
		a.report.Artifacts = append(a.report.Artifacts, x)
	}
	cf := a.p.reportDNS
	if cf == nil {
		if token := reportEnv(env, "CLOUDFLARE_API_TOKEN"); token != "" {
			cf = dns.NewClient(token)
		}
	}
	wants, e := nukeDNSInventory(state)
	if e != nil {
		a.warn("dns_inventory_unavailable")
		for _, known := range a.knownDNS {
			known.Observation = unknown("pulumi-state")
			a.report.Artifacts = append(a.report.Artifacts, known)
		}
		x := add("external:cloudflare:inventory", "DNS inventory unavailable", "cloudflare:dns", "cloudflare", "")
		a.report.Artifacts = append(a.report.Artifacts, x)
	}
	for _, w := range wants {
		x := add("external:cloudflare:"+dns.Name(w.Name)+":"+w.Type, w.Type+" "+dns.Name(w.Name), "cloudflare:dnsRecord", "cloudflare", dns.Name(w.Name)+"/"+w.Type)
		x.Priority = 10
		if cf == nil {
			x.Observation = reporting.Observation{Status: reporting.Unknown, Reason: reporting.Unchecked, Source: "cloudflare-auth-unavailable"}
		}
		if cf != nil {
			zones, e := cf.Zones(w.Name)
			if e == nil && len(zones) == 1 {
				records, e := cf.Records(zones[0].ID, w.Name)
				if e == nil {
					x.Observation.Status = reporting.Absent
					x.Observation.Reason = reporting.NotFound
					candidate := -1
					matching := false
					invalid := false
					for i, r := range records {
						if r.Type != w.Type || dns.Name(r.Name) != dns.Name(w.Name) {
							continue
						}
						if !dns.ValidID(r.ID) {
							invalid = true
							continue
						}
						equal := dns.Name(r.Content) == dns.Name(w.Content)
						if w.Type == "TXT" {
							equal = strings.Trim(r.Content, `"`) == strings.Trim(w.Content, `"`)
						}
						if candidate < 0 {
							candidate = i
						}
						if equal {
							candidate = i
							matching = true
							break
						}
					}
					if candidate >= 0 {
						x.Observation.Status = reporting.Present
						x.Observation.Reason = ""
						x.ProviderID = zones[0].ID + "/" + records[candidate].ID
						if !matching {
							a.warn("dns_configuration_mismatch:" + dns.Name(w.Name) + ":" + w.Type)
						}
					} else if invalid {
						x.Observation = unknown("cloudflare-api")
						a.warn("dns_record_identity_invalid")
					}

				}
			}
		}
		a.report.Artifacts = append(a.report.Artifacts, x)
	}
	if u, e := url.Parse(s.Backend); e == nil && u.Scheme == "s3" && u.Host != "" {
		x := add("external:state-bucket:"+u.Host, u.Host, "aws:s3/stateBucket", "aws", u.Host)
		args := []string{"s3api", "head-bucket", "--bucket", u.Host}
		if s.Account != "" {
			args = append(args, "--expected-bucket-owner", s.Account)
		}
		if _, e := a.r.Exec(a.root, true, "aws", args...); e == nil {
			x.Observation.Status = reporting.Present
			x.Observation.Reason = ""
		}
		a.report.Artifacts = append(a.report.Artifacts, x)
	}
	cp := artifactCheckpoint(a.root, s.Stack)
	role := cp.RoleARN
	if role == "" {
		role = reportEnv(env, "AWS_ROLE_ARN")
	}
	x := add("external:deploy-role:"+s.Environment, "Setup deployment role", "aws:iam/deployRole", "aws", role)
	if role == "" {
		a.warn("deploy_role_identity_unavailable")
		x.Observation = reporting.Observation{Status: reporting.Unknown, Reason: reporting.Unchecked, Source: "setup-checkpoint"}
	}
	if role != "" {
		name := role[strings.LastIndex(role, "/")+1:]
		b, e := a.r.Exec(a.root, true, "aws", "iam", "get-role", "--role-name", name, "--output", "json")
		var body struct{ Role struct{ Arn string } }
		if e == nil && json.Unmarshal(b, &body) == nil && body.Role.Arn == role {
			x.Observation.Status = reporting.Present
			x.Observation.Reason = ""
		}
	}
	a.report.Artifacts = append(a.report.Artifacts, x)
}

// A central hook also covers direct shared/setup Pulumi calls. Collection
// commands are export/refresh and therefore cannot enter this hook recursively.
func (p *processRunner) artifactCommand(dir, name string, args []string) func(error) {
	noop := func(error) {}
	if p.reportDisabled || name != "pulumi" || len(args) == 0 || (args[0] != "preview" && args[0] != "up") {
		return noop
	}
	if filepath.Base(dir) != "infra" && filepath.Base(dir) != "infra-index" {
		return noop
	}
	stack := ""
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--stack" {
			stack = args[i+1]
		}
	}
	if stack == "" {
		return noop
	}
	own := p.reportActive == nil
	if own {
		op := "preview"
		if args[0] == "up" {
			op = "deploy"
		}
		p.reportActive = beginArtifactAttempt(p, p, filepath.Dir(dir), dir, stack, op)
	}
	a := p.reportActive
	if args[0] == "up" {
		a.upAttempted = true
		a.report.Metadata.Operation = "deploy"
		a.persist()
	}
	return func(e error) {
		if args[0] == "up" {
			if e == nil {
				a.upSucceeded = true
			}
			a.checkNative()
			a.persist()
		}
		if own {
			a.finish(e)
			p.reportActive = nil
		}
	}
}

// Untaggable children receive metadata only from the exact native inventory
// row. Never bind a stale ID or copy arbitrary stack outputs into a report.
func overlayArtifactOwnership(as []reporting.Artifact, state []byte, repo string) []reporting.Artifact {
	var doc struct {
		Deployment struct {
			Resources []struct {
				Type    string
				Outputs struct {
					Inventory json.RawMessage `json:"resourceInventory"`
				}
			}
		}
	}
	if json.Unmarshal(state, &doc) != nil {
		return as
	}
	type row struct {
		URN, ID, Owner string
		Ownership      map[string]string
	}
	rows := map[string]row{}
	for _, r := range doc.Deployment.Resources {
		if r.Type != "pulumi:pulumi:Stack" {
			continue
		}
		var entries []row
		if json.Unmarshal(r.Outputs.Inventory, &entries) != nil {
			continue
		}
		for _, v := range entries {
			if v.URN != "" && v.ID != "" {
				rows[v.URN] = v
			}
		}
	}
	owners := map[string]string{"authentication": "github.com/lambdawalker/go.apexfission.aws.auth", "email-verification": "github.com/lambdawalker/go.apexfission.aws.verifyemail", "card-capture": "github.com/lambdawalker/go.appexfission.aws.card-capture"}
	for i, x := range as {
		if r, ok := rows[x.Key]; ok && r.ID == x.ProviderID {
			if x.Feature == "" {
				as[i].Feature = r.Ownership["ic:feature"]
			}
			if r.Owner != "" {
				as[i].Owner = r.Owner
			}
		}
		if as[i].Owner == "" {
			as[i].Owner = owners[as[i].Feature]
			if as[i].Owner == "" {
				as[i].Owner = repo
			}
		}
	}
	return as
}

func (a *artifactAttempt) checkNative() {
	a.state = a.readState(true)
	as, e := reportcli.CollectRefresh(context.Background(), reportRefreshRunner{a.r}, reportcli.RefreshOptions{Dir: a.dir, Stack: a.stack}, a.report.Artifacts)
	a.report.Artifacts = as
	a.nativeChecked = true
	if e != nil {
		a.warn("native_refresh_partial")
	}
}

func pulumiEventLogArg(args []string) string {
	for i, arg := range args {
		if arg == "--event-log" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "--event-log=") {
			return strings.TrimPrefix(arg, "--event-log=")
		}
	}
	return ""
}

// Keep only safe DNS identities across export attempts. An unavailable current
// inventory can invalidate evidence, but cannot establish that known names or
// exact record IDs disappeared. Raw exports and desired record contents are not
// retained as fallback evidence.
func (a *artifactAttempt) rememberDNSArtifacts(as []reporting.Artifact) {
	if a.knownDNS == nil {
		a.knownDNS = map[string]reporting.Artifact{}
	}
	for _, x := range as {
		if x.Provider == "cloudflare" && x.Type == "cloudflare:dnsRecord" {
			a.knownDNS[x.Key] = x
		}
	}
}
func (a *artifactAttempt) rememberDNSState(state []byte) {
	wants, e := nukeDNSInventory(state)
	if e != nil {
		return
	}
	if a.knownDNS == nil {
		a.knownDNS = map[string]reporting.Artifact{}
	}
	for _, w := range wants {
		key := "external:cloudflare:" + dns.Name(w.Name) + ":" + w.Type
		if _, known := a.knownDNS[key]; known {
			continue
		}
		a.knownDNS[key] = reporting.Artifact{Key: key, Name: w.Type + " " + dns.Name(w.Name), Type: "cloudflare:dnsRecord", Provider: "cloudflare", ProviderID: dns.Name(w.Name) + "/" + w.Type, Owner: a.report.Scope.Repository, Feature: "attestra-deployment", Priority: 10, Observation: reporting.Observation{Status: reporting.Unknown, Reason: reporting.Unchecked}}
	}
}
