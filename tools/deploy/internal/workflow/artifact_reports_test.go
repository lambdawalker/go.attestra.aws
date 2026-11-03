package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/dns"
	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/github"
	"github.com/lambdawalker/go.infra.composer/reporting"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArtifactCachedReportOfflineAndAmbiguity(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	r := reporting.Report{Scope: reporting.Scope{Application: "attestra", Repository: "owner/repo", Environment: "dev", Stack: "dev", Account: "123", Region: "us-east-2", Backend: "s3://one", Project: "attestra-application"}, Artifacts: []reporting.Artifact{{Key: "x", ProviderID: "physical", Observation: reporting.Observation{Status: reporting.Present, Source: "live", ObservedAt: now}}}}
	if e := reporting.SaveCache(reporting.CachePath(artifactCacheDir(root), r.Scope), r); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e := runArtifactReports(options{Root: root, Stack: "dev"}, artifactReportFlags{Report: true, Scope: "app"}, &out); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), "present") {
		t.Fatal(out.String())
	}
	loaded, e := loadArtifactScopes(root, options{Stack: "dev"}, artifactReportFlags{Scope: "app"})
	if e != nil || len(loaded) != 1 || !loaded[0].CacheLoaded || !loaded[0].Artifacts[0].Observation.ObservedAt.Equal(now) {
		t.Fatalf("%+v %v", loaded, e)
	}
	r.Scope.Account = "456"
	reporting.SaveCache(reporting.CachePath(artifactCacheDir(root), r.Scope), r)
	if _, e := loadArtifactScopes(root, options{Stack: "dev"}, artifactReportFlags{Scope: "app"}); e == nil {
		t.Fatal("ambiguous scope accepted")
	}
}

type reportFakeRunner struct {
	exportErr error
	calls     []string
	state     string
}

func (r *reportFakeRunner) Exec(dir string, capture bool, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	if name == "git" {
		return nil, nil
	}
	if name == "aws" {
		return []byte(`{}`), errors.New("denied SECRET")
	}
	if len(args) > 1 && args[0] == "stack" && args[1] == "export" {
		return []byte(r.state), r.exportErr
	}
	if len(args) > 0 && args[0] == "refresh" {
		for i, a := range args {
			if a == "--event-log" {
				os.WriteFile(args[i+1], []byte(`{"resourceOutputsEvent":{"metadata":{"op":"same","urn":"urn:pulumi:dev::attestra-application::aws:s3/bucket:Bucket::test","new":{"id":"bucket"}}}}`+"\n"), 0600)
			}
		}
		return nil, errors.New("partial")
	}
	return nil, nil
}
func TestArtifactFailureRetainsPartialLiveEvidence(t *testing.T) {
	root := t.TempDir()
	p := &processRunner{}
	r := &reportFakeRunner{state: `{"deployment":{"resources":[{"urn":"urn:pulumi:dev::attestra-application::aws:s3/bucket:Bucket::test","type":"aws:s3/bucket:Bucket","id":"bucket","custom":true,"outputs":{}},{"urn":"urn:pulumi:dev::attestra-application::aws:s3/bucket:Bucket::other","type":"aws:s3/bucket:Bucket","id":"other","custom":true,"outputs":{}}]}}`}
	a := beginArtifactAttempt(p, r, root, filepath.Join(root, "infra"), "dev", "deploy")
	a.upAttempted = true
	a.finish(errors.New("up failed SECRET"))
	var present, unknown bool
	for _, x := range a.report.Artifacts {
		if x.ProviderID == "bucket" {
			present = x.Observation.Status == reporting.Present
		}
		if x.ProviderID == "other" {
			unknown = x.Observation.Status == reporting.Unknown
		}
	}
	if !present || !unknown {
		t.Fatalf("partial evidence lost %+v", a.report)
	}
	b, e := os.ReadFile(filepath.Join(a.output, "artifacts.json"))
	if e != nil || bytes.Contains(b, []byte("SECRET")) {
		t.Fatalf("%s %v", b, e)
	}
	if a.report.Metadata.Outcome != "failed" {
		t.Fatal(a.report.Metadata)
	}
	for _, ext := range []string{"md", "json", "html"} {
		s, e := os.Stat(filepath.Join(a.output, "artifacts."+ext))
		if e != nil || s.Mode().Perm() != 0600 {
			t.Fatalf("private bundle %v %v", s, e)
		}
	}
}
func TestArtifactFlagsRejectWriteCombinations(t *testing.T) {
	for _, flags := range [][]string{{"report", "bootstrap"}, {"preview", "publish-index"}, {"refresh"}, {"report", "preview"}, {"preview", "pull"}} {
		if validateArtifactFlags(artifactReportFlags{Report: containsReportFlag(flags, "report"), Preview: containsReportFlag(flags, "preview"), Refresh: containsReportFlag(flags, "refresh"), Scope: "app"}, flags) == nil {
			t.Fatalf("accepted %v", flags)
		}
	}
}

func TestArtifactHooksCoverNativeEntrypoints(t *testing.T) {
	for _, dir := range []string{"infra", "infra-index"} {
		t.Run(dir, func(t *testing.T) {
			root := t.TempDir()
			r := &reportFakeRunner{state: `{"deployment":{"resources":[]}}`}
			p := &processRunner{reportRunner: r, reportAccount: "123456789012", env: []string{"AWS_REGION=us-east-2", "PULUMI_BACKEND_URL=s3://state"}}
			stack := "dev"
			if dir == "infra-index" {
				stack = "shared"
			}
			finish := p.artifactCommand(filepath.Join(root, dir), "pulumi", []string{"up", "--stack", stack})
			if p.reportActive == nil {
				t.Fatal("up missing early reporting")
			}
			if _, e := os.Stat(filepath.Join(p.reportActive.output, "artifacts.json")); e != nil {
				t.Fatal(e)
			}
			finish(errors.New("up failed"))
			if p.lastReport == nil || p.lastReport.report.Metadata.Outcome != "failed" {
				t.Fatal("failed native deploy missing final report")
			}
			var refresh bool
			for _, call := range r.calls {
				if strings.HasPrefix(call, "pulumi refresh --preview-only") {
					refresh = true
				}
				if strings.Contains(call, "destroy") || strings.Contains(call, "put-object") {
					t.Fatalf("report collector mutated state: %s", call)
				}
			}
			if !refresh {
				t.Fatal("failed up did not refresh")
			}
		})
	}
}
func TestArtifactPreviewHookDoesNotMutateOrOverwriteLiveCache(t *testing.T) {
	root := t.TempDir()
	r := &reportFakeRunner{state: `{"deployment":{"resources":[]}}`}
	p := &processRunner{reportRunner: r, reportAccount: "123456789012", env: []string{"AWS_REGION=us-east-2", "PULUMI_BACKEND_URL=s3://state"}}
	finish := p.artifactCommand(filepath.Join(root, "infra"), "pulumi", []string{"preview", "--stack", "dev"})
	finish(nil)
	for _, call := range r.calls {
		if strings.Contains(call, "refresh") || strings.Contains(call, "put-") || strings.Contains(call, " up") {
			t.Fatal(call)
		}
	}
	files, _ := filepath.Glob(filepath.Join(artifactCacheDir(root), "*.json"))
	if len(files) != 0 {
		t.Fatal("preview replaced live cache")
	}
}
func TestArtifactInvalidStateRetainsKnownInventoryUnknown(t *testing.T) {
	root := t.TempDir()
	r := &reportFakeRunner{state: `{"deployment":{"resources":[]}}`}
	p := &processRunner{}
	a := beginArtifactAttempt(p, r, root, filepath.Join(root, "infra"), "dev", "deploy")
	a.cached = reporting.Report{Artifacts: []reporting.Artifact{{Key: "known", ProviderID: "id", Provider: "aws", Observation: reporting.Observation{Status: reporting.Present, ObservedAt: time.Now()}}}}
	r.state = `{"bad":"secret"}`
	a.readState(true)
	if len(a.report.Artifacts) != 1 || a.report.Artifacts[0].Observation.Status != reporting.Unknown {
		t.Fatalf("%+v", a.report.Artifacts)
	}
}
func TestArtifactOwnershipExactInventory(t *testing.T) {
	as := []reporting.Artifact{{Key: "urn:one", ProviderID: "id"}, {Key: "urn:stale", ProviderID: "new"}}
	state := []byte(`{"deployment":{"resources":[{"type":"pulumi:pulumi:Stack","outputs":{"resourceInventory":[{"urn":"urn:one","id":"id","owner":"feature-repo","ownership":{"ic:feature":"authentication"}},{"urn":"urn:stale","id":"old","owner":"wrong","ownership":{"ic:feature":"authentication"}}]}}]}}`)
	got := overlayArtifactOwnership(as, state, "app")
	if got[0].Owner != "feature-repo" || got[0].Feature != "authentication" || got[1].Owner != "app" || got[1].Feature != "" {
		t.Fatal(got)
	}
}

func TestArtifactGitHubCompleteListAndObscured404(t *testing.T) {
	for _, mode := range []string{"present-page2", "absent", "404", "403", "malformed", "missing-token"} {
		t.Run(mode, func(t *testing.T) {
			pages := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("write request", r.Method)
				}
				pages++
				switch mode {
				case "404":
					w.WriteHeader(404)
					return
				case "403":
					w.WriteHeader(403)
					return
				case "malformed":
					fmt.Fprint(w, `{}`)
					return
				}
				var names []map[string]string
				if r.URL.Query().Get("page") == "1" && mode == "present-page2" {
					for i := 0; i < 100; i++ {
						names = append(names, map[string]string{"name": fmt.Sprintf("environment%d", i)})
					}
				} else if mode == "present-page2" {
					names = []map[string]string{{"name": "dev"}}
				} else {
					names = []map[string]string{}
				}
				json.NewEncoder(w).Encode(map[string]any{"environments": names})
			}))
			defer srv.Close()
			g := github.NewClient("test")
			g.BaseURL = srv.URL
			p := &processRunner{env: []string{}, reportGitHub: g}
			if mode == "missing-token" {
				p.reportGitHub = nil
			}
			a := &artifactAttempt{p: p, r: &reportFakeRunner{}, root: t.TempDir(), report: reporting.Report{Scope: reporting.Scope{Repository: "owner/repo", Environment: "dev", Project: "attestra-application"}}}
			a.supplemental([]byte(`{"deployment":{"resources":[]}}`))
			var got reporting.Status
			for _, x := range a.report.Artifacts {
				if x.Provider == "github" {
					got = x.Observation.Status
				}
			}
			want := reporting.Unknown
			if mode == "present-page2" {
				want = reporting.Present
				if pages != 2 {
					t.Fatal(pages)
				}
			}
			if mode == "absent" {
				want = reporting.Absent
			}
			if got != want {
				t.Fatalf("got %s want %s", got, want)
			}
			if mode == "missing-token" && pages != 0 {
				t.Fatal("queried without token")
			}
		})
	}
}
func TestArtifactCloudflarePresenceMismatchAndPermissions(t *testing.T) {
	for _, mode := range []string{"present", "absent", "mismatch", "403", "missing-token"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" {
					t.Error("write request")
				}
				if mode == "403" {
					w.WriteHeader(403)
					fmt.Fprint(w, `{"success":false}`)
					return
				}
				if r.URL.Path == "/zones" {
					fmt.Fprint(w, `{"success":true,"result":[{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","name":"example.com","status":"active"}]}`)
					return
				}
				records := []dns.Record{}
				if mode != "absent" {
					content := "target.example.net"
					if mode == "mismatch" {
						content = "other.example.net"
					}
					records = append(records, dns.Record{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Name: "api.example.com", Type: "CNAME", Content: content})
				}
				json.NewEncoder(w).Encode(map[string]any{"success": true, "result": records})
			}))
			defer srv.Close()
			cf := dns.NewClient("test")
			cf.BaseURL = srv.URL
			p := &processRunner{env: []string{}, reportDNS: cf}
			if mode == "missing-token" {
				p.reportDNS = nil
			}
			a := &artifactAttempt{p: p, r: &reportFakeRunner{}, root: t.TempDir(), report: reporting.Report{Scope: reporting.Scope{Repository: "owner/repo", Environment: "dev", Project: "attestra-application"}}}
			a.supplemental([]byte(`{"deployment":{"resources":[{"type":"aws:apigatewayv2/domainName:DomainName","outputs":{"domainName":"api.example.com","domainNameConfiguration":{"targetDomainName":"target.example.net"}}}]}}`))
			var got reporting.Status
			for _, x := range a.report.Artifacts {
				if x.Provider == "cloudflare" {
					got = x.Observation.Status
				}
			}
			want := reporting.Present
			if mode == "absent" {
				want = reporting.Absent
			}
			if mode == "403" || mode == "missing-token" {
				want = reporting.Unknown
			}
			if got != want {
				t.Fatalf("got %s want %s", got, want)
			}
			if mode == "mismatch" && !containsReportFlag(a.report.Warnings, "dns_configuration_mismatch:api.example.com:CNAME") {
				t.Fatal(a.report.Warnings)
			}
			if mode == "missing-token" && calls != 0 {
				t.Fatal("queried without token")
			}
		})
	}
}
func TestArtifactRefreshDeletesPrivateEvents(t *testing.T) {
	root := t.TempDir()
	r := &reportFakeRunner{state: `{"deployment":{"resources":[]}}`}
	a := beginArtifactAttempt(&processRunner{}, r, root, filepath.Join(root, "infra"), "dev", "deploy")
	a.finish(nil)
	for _, call := range r.calls {
		parts := strings.Fields(call)
		for i, v := range parts {
			if v == "--event-log" {
				if _, e := os.Stat(parts[i+1]); !os.IsNotExist(e) {
					t.Fatalf("raw event log retained: %v", e)
				}
			}
		}
	}
}

func TestArtifactExistingEventLogArgument(t *testing.T) {
	for _, args := range [][]string{{"preview", "--event-log", "private.json"}, {"preview", "--event-log=private.json"}} {
		if got := pulumiEventLogArg(args); got != "private.json" {
			t.Fatal(got)
		}
	}
}
func TestArtifactReplacementDoesNotReuseOldPhysicalEvidence(t *testing.T) {
	root := t.TempDir()
	old := reporting.Artifact{Key: "urn:pulumi:dev::attestra-application::aws:s3/bucket:Bucket::test", Provider: "aws", ProviderID: "old", Observation: reporting.Observation{Status: reporting.Present, ObservedAt: time.Now()}}
	r := &reportFakeRunner{state: `{"deployment":{"resources":[{"urn":"urn:pulumi:dev::attestra-application::aws:s3/bucket:Bucket::test","type":"aws:s3/bucket:Bucket","id":"new","custom":true}]}}`}
	a := &artifactAttempt{p: &processRunner{}, r: r, root: root, dir: filepath.Join(root, "infra"), stack: "dev", report: reporting.Report{Artifacts: []reporting.Artifact{old}}}
	a.readState(true)
	if len(a.report.Artifacts) != 2 {
		t.Fatal(a.report.Artifacts)
	}
	for _, x := range a.report.Artifacts {
		if x.ProviderID == "new" && x.Observation.Status == reporting.Present {
			t.Fatal("new ID inherited old presence")
		}
		if x.ProviderID == "old" && (!strings.Contains(x.PlannedAction, "replacement") || x.Observation.Status != reporting.Unknown) {
			t.Fatal("old identity lost", x)
		}
	}
}

func TestArtifactFinishRetainsKnownDNSAfterExportFailure(t *testing.T) {
	for _, mode := range []string{"failed", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			before := time.Now().UTC().Add(-time.Hour)
			p := &processRunner{reportAccount: "123456789012", env: []string{"AWS_REGION=us-east-2", "PULUMI_BACKEND_URL=s3://state"}}
			dir := filepath.Join(root, "infra")
			scope := artifactScope(p, root, dir, "dev")
			cachedDNS := reporting.Artifact{Key: "external:cloudflare:cached.example.com:CNAME", Name: "CNAME cached.example.com", Type: "cloudflare:dnsRecord", Provider: "cloudflare", ProviderID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Owner: scope.Repository, Feature: "attestra-deployment", Observation: reporting.Observation{Status: reporting.Present, Source: "cloudflare-api", ObservedAt: before}}
			cache := reporting.CachePath(artifactCacheDir(root), scope)
			if e := reporting.SaveCache(cache, reporting.Report{Scope: scope, Artifacts: []reporting.Artifact{cachedDNS}}); e != nil {
				t.Fatal(e)
			}
			r := &reportFakeRunner{state: `{"deployment":{"resources":[{"urn":"urn:pulumi:dev::attestra-application::aws:apigatewayv2/domainName:DomainName::api","type":"aws:apigatewayv2/domainName:DomainName","id":"api.example.com","custom":true,"outputs":{"domainName":"initial.example.com","domainNameConfiguration":{"targetDomainName":"target.example.net"}}}]}}`}
			a := beginArtifactAttempt(p, r, root, dir, "dev", "deploy")
			if mode == "failed" {
				r.exportErr = errors.New("unavailable SECRET")
			} else {
				r.state = `{"invalid":"SECRET"}`
			}
			startedFailure := time.Now().UTC()
			a.upAttempted = true
			a.finish(errors.New("up failed"))
			bundle, e := os.ReadFile(filepath.Join(a.output, "artifacts.json"))
			if e != nil {
				t.Fatal(e)
			}
			if bytes.Contains(bundle, []byte("SECRET")) {
				t.Fatal("diagnostics leaked")
			}
			var rendered reporting.Report
			if e := json.Unmarshal(bundle, &rendered); e != nil {
				t.Fatal(e)
			}
			saved, e := reporting.LoadCache(cache, scope)
			if e != nil {
				t.Fatal(e)
			}
			for _, check := range []struct {
				name   string
				report reporting.Report
			}{{"bundle", rendered}, {"cache", saved}} {
				expected := map[string]string{cachedDNS.Key: cachedDNS.ProviderID, "external:cloudflare:initial.example.com:CNAME": "initial.example.com/CNAME"}
				for _, x := range check.report.Artifacts {
					if id, ok := expected[x.Key]; ok {
						if x.ProviderID != id || x.Observation.Status != reporting.Unknown || x.Observation.Reason != reporting.CheckFailed || x.Observation.ObservedAt.Before(startedFailure) {
							t.Fatalf("%s lost failed identity evidence: %+v", check.name, x)
						}
						delete(expected, x.Key)
					}
				}
				if len(expected) != 0 {
					t.Fatalf("%s lost known DNS identities: %v", check.name, expected)
				}
			}
		})
	}
}
