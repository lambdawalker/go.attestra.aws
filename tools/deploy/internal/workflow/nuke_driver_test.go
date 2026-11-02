package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	awsenv "github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/aws"
	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/dns"
	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/github"
	"github.com/lambdawalker/go.infra.composer/teardown"
	"github.com/lambdawalker/go.infra.composer/teardown/pulumicli"
)

type nukeAdapterAWS func(any, ...string) (bool, error)

func (f nukeAdapterAWS) Call(v any, args ...string) (bool, error) { return f(v, args...) }
func nukeDecode(v any, body string) (bool, error) {
	if v != nil {
		return true, json.Unmarshal([]byte(body), v)
	}
	return true, nil
}
func nukeReadOnlyAWS(t *testing.T) func(string) awsenv.API {
	return func(string) awsenv.API {
		return nukeAdapterAWS(func(v any, args ...string) (bool, error) {
			switch strings.Join(args[:2], " ") {
			case "sts get-caller-identity":
				return nukeDecode(v, `{"Account":"123456789012"}`)
			case "iam list-roles":
				return nukeDecode(v, `{"Roles":[],"IsTruncated":false}`)
			case "s3api list-buckets":
				return nukeDecode(v, `{"Buckets":[]}`)
			case "s3api get-bucket-location":
				return nukeDecode(v, `{"LocationConstraint":"us-east-2"}`)
			case "s3api get-bucket-encryption":
				return nukeDecode(v, `{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}}`)
			case "s3api get-bucket-tagging":
				return nukeDecode(v, `{"TagSet":[{"Key":"ic:application","Value":"attestra"},{"Key":"ic:environment","Value":"shared"},{"Key":"ic:component","Value":"pulumi-state"}]}`)
			default:
				t.Fatalf("unexpected or mutating call: %v", args)
				return false, nil
			}
		})
	}
}
func nukeEmptyGitHub(t *testing.T) *github.Client {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("mutation during discovery: %s", r.Method)
		}
		_, _ = w.Write([]byte(`{"environments":[]}`))
	}))
	t.Cleanup(s.Close)
	g := github.NewClient("test")
	g.BaseURL = s.URL
	return g
}
func TestComposerNukeDiscoveryUsesCurrentProjectsWithoutInventedDev(t *testing.T) {
	root := t.TempDir()
	if e := os.Mkdir(filepath.Join(root, ".attestra"), 0700); e != nil {
		t.Fatal(e)
	}
	record := `{"Repository":"owner/repo","Environment":"qa","Account":"123456789012","Backend":"s3://attestra-state-test","Region":"us-east-2"}`
	if e := os.WriteFile(filepath.Join(root, ".attestra", "bootstrap.qa.local.json"), []byte(record), 0600); e != nil {
		t.Fatal(e)
	}
	d, e := collectNukeDiscovery(root, "owner/repo", "123456789012", nukeEmptyGitHub(t), nukeReadOnlyAWS(t))
	if e != nil {
		t.Fatal(e)
	}
	if len(d.config.Projects) != 2 || d.config.Projects[0].Name != "attestra-application" || d.config.Projects[0].Directory != filepath.Join(root, "infra") || d.config.Projects[1].Name != "attestra-control-plane" || !d.config.Projects[1].Shared {
		t.Fatalf("wrong projects: %+v", d.config.Projects)
	}
	if len(d.artifacts) != 1 || d.artifacts[0].ID != "qa" {
		t.Fatalf("lost local environment after GitHub removal: %+v", d.artifacts)
	}
	if len(d.config.Recovery) != 1 || d.config.Recovery[0].Data["exclusive"] != "true" {
		t.Fatal("owned bucket recovery not captured")
	}
	empty, e := collectNukeDiscovery(t.TempDir(), "owner/repo", "123456789012", nukeEmptyGitHub(t), nukeReadOnlyAWS(t))
	if e != nil || len(empty.config.Projects) != 0 {
		t.Fatalf("invented backend: %+v %v", empty, e)
	}
}
func TestComposerNukeDiscoveryRejectsMalformedSourceAndAccountChange(t *testing.T) {
	for _, record := range []string{`{`, `{"Repository":"owner/repo","Account":"999999999999","Backend":"s3://state","Region":"us-east-2"}`} {
		root := t.TempDir()
		_ = os.Mkdir(filepath.Join(root, ".attestra"), 0700)
		_ = os.WriteFile(filepath.Join(root, ".attestra", "bootstrap.qa.local.json"), []byte(record), 0600)
		if _, e := collectNukeDiscovery(root, "owner/repo", "123456789012", nukeEmptyGitHub(t), nukeReadOnlyAWS(t)); e == nil {
			t.Fatal("unsafe local record accepted")
		}
	}
}
func TestComposerNukeGitHubMalformedAnd404AreUnverified(t *testing.T) {
	for _, body := range []string{`{}`, `{"environments":null}`, "404"} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if body == "404" {
				w.WriteHeader(404)
				return
			}
			_, _ = w.Write([]byte(body))
		}))
		g := github.NewClient("test")
		g.BaseURL = s.URL
		d := &composerNukeDriver{Repository: "owner/repo", Account: "123456789012", GitHub: g, AWSForRegion: nukeReadOnlyAWS(t)}
		a := teardown.Artifact{ID: "qa", Kind: "attestra_github_environment", Data: map[string]string{"account": d.Account, "repository": d.Repository, "environment": "qa"}}
		if state, e := d.Inspect(context.Background(), a); e == nil || state == teardown.StateAbsent {
			t.Fatalf("inaccessible GitHub implies absence: %v %v", state, e)
		}
		s.Close()
	}
}
func TestComposerNukeRefusesCallerAndForeignDeploymentRole(t *testing.T) {
	d := &composerNukeDriver{Account: "123456789012", CallerARN: "arn:aws:sts::123456789012:assumed-role/deployer/session", Repository: "owner/repo", AWSForRegion: nukeReadOnlyAWS(t)}
	for _, arn := range []string{"arn:aws:iam::123456789012:role/deployer", "arn:aws:iam::999999999999:role/other"} {
		a := teardown.Artifact{ID: arn, Kind: "attestra_deployment_role", Data: map[string]string{"account": d.Account, "repository": d.Repository, "environment": "qa"}}
		if e := d.Cleanup(context.Background(), a); e == nil {
			t.Fatal("unsafe role deletion allowed")
		}
	}
}
func TestComposerNukeRetainsActiveBootstrapLock(t *testing.T) {
	readOnly := nukeReadOnlyAWS(t)
	d := &composerNukeDriver{Account: "123456789012", AWSForRegion: func(region string) awsenv.API {
		return nukeAdapterAWS(func(v any, args ...string) (bool, error) {
			if len(args) > 1 && args[1] == "put-object" {
				if !strings.Contains(strings.Join(args, " "), "--if-none-match *") {
					t.Fatal("lock was not conditional")
				}
				return false, &awsenv.Error{Code: "PreconditionFailed"}
			}
			return readOnly(region).Call(v, args...)
		})
	}}
	a := teardown.Artifact{ID: "attestra-control-plane-state-123456789012-us-east-2/bootstrap.lock", Kind: "attestra_marker", Data: map[string]string{"account": d.Account, "bucket": "attestra-control-plane-state-123456789012-us-east-2", "project": "attestra-control-plane", "stack": "shared", "key": "bootstrap.lock"}}
	if e := d.Cleanup(context.Background(), a); e == nil {
		t.Fatal("active bootstrap lock removed")
	}
}
func TestComposerNukeDNSReadOnlyFailureDoesNotAuthorizeAbsence(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Fatal("check mutated DNS")
		}
		w.WriteHeader(403)
	}))
	defer s.Close()
	cf := dns.NewClient("test")
	cf.BaseURL = s.URL
	d := &composerNukeDriver{Account: "123456789012", Repository: "owner/repo", Cloudflare: cf, AWSForRegion: nukeReadOnlyAWS(t)}
	a := teardown.Artifact{ID: "api.example.com", Kind: "attestra_dns", Data: map[string]string{"account": d.Account, "repository": d.Repository, "zone": strings.Repeat("a", 32), "type": "CNAME", "content": "target.example.com"}}
	if state, e := d.Inspect(context.Background(), a); e == nil || state == teardown.StateAbsent {
		t.Fatalf("DNS denied interpreted absent: %s %v", state, e)
	}
}
func TestComposerNukeDNSInventoryIgnoresExternalAndRoute53Alias(t *testing.T) {
	b := []byte(`{"deployment":{"resources":[{"type":"aws:cloudfront/distribution:Distribution","outputs":{"aliases":["app.example.com"],"domainName":"cdn.example.net"}},{"type":"aws:route53/record:Record","outputs":{"name":"app.example.com","type":"A"}},{"external":true,"type":"aws:acm/certificate:Certificate","outputs":{"domainValidationOptions":[{"ResourceRecordName":"foreign.example.com","ResourceRecordType":"CNAME","ResourceRecordValue":"validation.example.net"}]}}]}}`)
	records, e := nukeDNSInventory(b)
	if e != nil || len(records) != 0 {
		t.Fatalf("foreign/managed DNS captured: %+v %v", records, e)
	}
	if _, e = nukeDNSInventory([]byte(`{}`)); e == nil {
		t.Fatal("invalid inventory accepted")
	}
}
func TestComposerNukeAccountChangeBlocksApplicationCheck(t *testing.T) {
	d := &composerNukeDriver{Account: "123456789012", AWSForRegion: func(string) awsenv.API {
		return nukeAdapterAWS(func(v any, args ...string) (bool, error) {
			if args[0] != "sts" {
				t.Fatal("called resource after account changed")
			}
			return false, errors.New("denied")
		})
	}}
	if _, e := d.Inspect(context.Background(), teardown.Artifact{Kind: "attestra_marker"}); e == nil {
		t.Fatal("account check bypassed")
	}
}

func TestComposerNukeBucketEncryptionRetainsUnprovenCustomerKey(t *testing.T) {
	a := nukeAdapterAWS(func(v any, args ...string) (bool, error) {
		switch args[0] {
		case "s3api":
			return nukeDecode(v, `{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"aws:kms","KMSMasterKeyID":"alias/state"}}]}}`)
		case "kms":
			if args[1] != "describe-key" {
				t.Fatal("key mutation during discovery")
			}
			return nukeDecode(v, `{"KeyMetadata":{"Arn":"arn:aws:kms:us-east-2:123456789012:key/test","AWSAccountId":"123456789012","KeyManager":"CUSTOMER"}}`)
		}
		t.Fatalf("unexpected command: %v", args)
		return false, nil
	})
	keys, e := nukeBucketKeys(a, "123456789012", "state", "us-east-2")
	if e != nil || len(keys) != 1 || keys[0].Data["exclusive"] != "false" || !keys[0].Recovery {
		t.Fatalf("unproven key exclusivity: %+v %v", keys, e)
	}
}
func TestComposerNukePulumiKeyCapturesRegionRejectsUnknownProviders(t *testing.T) {
	b := []byte(`{"deployment":{"secrets_providers":{"type":"cloud","state":"{\"url\":\"awskms://alias/state?region=us-west-2\"}"}}}`)
	key, e := nukePulumiKey(b, "us-east-2")
	if e != nil || key.ID != "alias/state" || key.Region != "us-west-2" {
		t.Fatalf("lost KMS evidence: %+v %v", key, e)
	}
	if _, e = nukePulumiKey([]byte(`{"deployment":{"secrets_providers":{"type":"cloud","state":{"url":"azurekeyvault://other"}}}}`), "us-east-2"); e == nil {
		t.Fatal("unknown cloud key discarded")
	}
}
func TestComposerNukeRoleDiscoverySurvivesMissingEnvironment(t *testing.T) {
	d := &composerNukeDriver{Account: "123456789012", Repository: "owner/repo"}
	d.Metadata.ID = 12
	d.Metadata.Name = "repo"
	d.Metadata.Owner.ID = 34
	d.Metadata.Owner.Login = "owner"
	d.AWSForRegion = func(string) awsenv.API {
		return nukeAdapterAWS(func(v any, args ...string) (bool, error) {
			switch args[1] {
			case "list-roles":
				return nukeDecode(v, `{"Roles":[{"RoleName":"attestra-github-deploy-qa","Arn":"arn:aws:iam::123456789012:role/attestra-github-deploy-qa"}],"IsTruncated":false}`)
			case "get-role":
				return nukeDecode(v, `{"Role":{"Arn":"arn:aws:iam::123456789012:role/attestra-github-deploy-qa","Tags":[{"Key":"attestra-setup","Value":"github-qa"},{"Key":"attestra-subject","Value":"repo:owner@34/repo@12:environment:qa"}]}}`)
			}
			t.Fatalf("unexpected call: %v", args)
			return false, nil
		})
	}
	if e := d.discoverRoles(); e != nil || len(d.extras) != 1 || d.extras[0].Kind != "attestra_deployment_role" {
		t.Fatalf("lost surviving role: %+v %v", d.extras, e)
	}
}

func TestComposerNukeMarkerVersionsPreflightBeforeDelete(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(itoaNuke(map[bool]int{false: 0, true: 1}[foreign]), func(t *testing.T) {
			removed, released, read := 0, false, 0
			readOnly := nukeReadOnlyAWS(t)
			d := &composerNukeDriver{Account: "123456789012"}
			d.AWSForRegion = func(region string) awsenv.API {
				return nukeAdapterAWS(func(v any, args ...string) (bool, error) {
					switch args[1] {
					case "put-object":
						if !strings.Contains(strings.Join(args, " "), "--if-none-match *") {
							t.Fatal("non-conditional lease")
						}
						return nukeDecode(v, `{"ETag":"lease-etag","VersionId":"own-lease"}`)
					case "list-object-versions":
						return nukeDecode(v, `{"Versions":[{"Key":"bootstrap-ready.json","VersionId":"v1"},{"Key":"bootstrap-ready.json","VersionId":"v2"}],"DeleteMarkers":[{"Key":"bootstrap-ready.json","VersionId":"deleted"}],"IsTruncated":false}`)
					case "get-object":
						read++
						body := `{"sourceHash":"hash","domain":"admin.example.com","zone":"zone"}`
						if foreign && read == 2 {
							body = `{"application":"foreign"}`
						}
						if e := os.WriteFile(args[len(args)-1], []byte(body), 0600); e != nil {
							t.Fatal(e)
						}
						return true, nil
					case "delete-objects":
						if read != 2 {
							t.Fatal("deleted before checking every version")
						}
						removed++
						if !strings.Contains(strings.Join(args, " "), `"VersionId":"v1"`) {
							t.Fatal("missing exact version deletion")
						}
						return nukeDecode(v, `{"Errors":[]}`)
					case "delete-object":
						if !strings.Contains(strings.Join(args, " "), "--version-id own-lease") {
							t.Fatal("removed foreign lock version")
						}
						released = true
						return true, nil
					default:
						return readOnly(region).Call(v, args...)
					}
				})
			}
			a := teardown.Artifact{ID: "attestra-control-plane-state-123456789012-us-east-2/bootstrap-ready.json", Kind: "attestra_marker", Region: "us-east-2", Data: map[string]string{"account": d.Account, "bucket": "attestra-control-plane-state-123456789012-us-east-2", "project": "attestra-control-plane", "stack": "shared", "key": "bootstrap-ready.json"}}
			e := d.Cleanup(context.Background(), a)
			if foreign {
				if e == nil || removed != 0 {
					t.Fatal("foreign version deleted")
				}
			} else if e != nil || removed != 1 {
				t.Fatalf("owned versions not deleted: %v count=%d", e, removed)
			}
			if !released {
				t.Fatal("own lock lease not released")
			}
		})
	}
}

type nukeAdapterRunner func(context.Context, string, []string, string, ...string) ([]byte, error)

func (f nukeAdapterRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	return f(ctx, dir, env, name, args...)
}
func TestComposerNukeEngineCleansSeededExternalArtifactsAndChecksReadOnly(t *testing.T) {
	present, deletes := true, 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			if !strings.HasSuffix(r.URL.Path, "/environments/qa") {
				t.Errorf("unexpected deletion %s", r.URL.Path)
			}
			deletes++
			present = false
			w.WriteHeader(204)
			return
		}
		if r.Method != "GET" {
			t.Errorf("unexpected mutation: %s", r.Method)
		}
		if strings.HasSuffix(r.URL.Path, "/environments") {
			if present {
				_, _ = w.Write([]byte(`{"environments":[{"name":"qa"}]}`))
			} else {
				_, _ = w.Write([]byte(`{"environments":[]}`))
			}
			return
		}
		if strings.HasSuffix(r.URL.Path, "/environments/qa") {
			if !present {
				w.WriteHeader(404)
			} else {
				_, _ = w.Write([]byte(`{}`))
			}
			return
		}
		vars := map[string]string{"AWS_ACCOUNT_ID": "123456789012", "PULUMI_BACKEND_URL": "s3://attestra-state-test", "AWS_REGION": "us-east-2", "PULUMI_STACK": "qa"}
		for key, value := range vars {
			if strings.HasSuffix(r.URL.Path, "/variables/"+key) {
				_ = json.NewEncoder(w).Encode(map[string]string{"value": value})
				return
			}
		}
		w.WriteHeader(404)
	}))
	defer s.Close()
	g := github.NewClient("test")
	g.BaseURL = s.URL
	scope := teardown.Scope{Application: "attestra", Account: "123456789012"}
	root := t.TempDir()
	base, e := pulumicli.New(pulumicli.Config{Scope: scope, Projects: []pulumicli.Project{{Name: "attestra-application", Directory: filepath.Join(root, "infra"), Backend: "s3://attestra-state-test", Region: "us-east-2"}}}, nukeAdapterRunner(func(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
		if name == "aws" && args[0] == "sts" {
			return []byte(`{"Account":"123456789012"}`), nil
		}
		if name == "pulumi" && len(args) > 1 && args[0] == "stack" && args[1] == "ls" {
			return []byte(`[]`), nil
		}
		t.Errorf("unexpected base command %s %v", name, args)
		return nil, errors.New("unexpected command")
	}))
	if e != nil {
		t.Fatal(e)
	}
	d := &composerNukeDriver{Base: base, Root: root, Repository: "owner/repo", Account: scope.Account, GitHub: g, AWSForRegion: nukeReadOnlyAWS(t)}
	if e = d.DiscoverArtifacts(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(d.AdditionalArtifacts()) != 1 || d.AdditionalArtifacts()[0].Data["cleanup"] != "true" {
		t.Fatalf("external artifact not opted into engine cleanup: %+v", d.AdditionalArtifacts())
	}
	store := &teardown.FileStore{Path: filepath.Join(root, "report.json")}
	if e = store.Save(&teardown.Report{Version: 1, Scope: scope, Artifacts: d.AdditionalArtifacts()}); e != nil {
		t.Fatal(e)
	}
	engine := &teardown.Engine{Scope: scope, Driver: d, Store: store}
	report, e := engine.Nuke(context.Background())
	if e != nil || deletes != 1 || present {
		t.Fatalf("engine failed external cleanup: %v deletes=%d report=%+v", e, deletes, report)
	}
	if len(report.Results) != 1 || report.Results[0].Status != teardown.Deleted {
		t.Fatalf("cleanup was not independently read back: %+v", report.Results)
	}
	if _, e = engine.Check(context.Background(), false); e != nil {
		t.Fatal(e)
	}
	if _, e = engine.Check(context.Background(), true); e != nil {
		t.Fatal(e)
	}
	if deletes != 1 {
		t.Fatal("check performed mutation")
	}
}

func TestComposerNukeInventoryPreservesEachDNSValueAndUsesCredentialHook(t *testing.T) {
	root := t.TempDir()
	scope := teardown.Scope{Application: "attestra", Account: "123456789012"}
	p := pulumicli.Project{Name: "attestra-application", Directory: filepath.Join(root, "infra"), Backend: "s3://state", Region: "us-east-2"}
	before := false
	state := []byte(`{"deployment":{"resources":[{"urn":"urn:pulumi:qa::attestra-application::aws:acm/certificate:Certificate::certificate","type":"aws:acm/certificate:Certificate","custom":true,"id":"arn:aws:acm:us-east-2:123456789012:certificate/test","outputs":{"domainValidationOptions":[{"ResourceRecordName":"same.example.com","ResourceRecordType":"TXT","ResourceRecordValue":"first"},{"ResourceRecordName":"same.example.com","ResourceRecordType":"TXT","ResourceRecordValue":"second"}]}}]}}`)
	base, e := pulumicli.New(pulumicli.Config{Scope: scope, Projects: []pulumicli.Project{p}}, nukeAdapterRunner(func(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
		if !before {
			t.Fatal("export used before credentials hook")
		}
		if name == "aws" && args[0] == "sts" {
			return []byte(`{"Account":"123456789012"}`), nil
		}
		if name == "pulumi" && args[0] == "stack" {
			switch args[1] {
			case "ls":
				return []byte(`[{"name":"qa"}]`), nil
			case "export":
				return state, nil
			}
		}
		t.Fatalf("inventory mutation or unexpected command: %s %v", name, args)
		return nil, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	d := &composerNukeDriver{Base: base, Account: scope.Account, Repository: "owner/repo", BeforeInventory: func(teardown.Target) error { before = true; return nil }}
	target := teardown.Target{ID: p.Backend + "|" + p.Name + "|qa", Project: p.Name, Stack: "qa", Environment: "qa", Directory: p.Directory, Backend: p.Backend, Region: p.Region}
	artifacts, e := d.Inventory(context.Background(), target)
	if e != nil {
		t.Fatal(e)
	}
	keys := map[string]bool{}
	count := 0
	for _, a := range artifacts {
		if a.Kind != "attestra_dns" {
			continue
		}
		count++
		if keys[a.Key()] {
			t.Fatal("different desired values share persisted artifact identity")
		}
		keys[a.Key()] = true
		if a.Data["cleanup"] != "true" || a.Data["name"] != "same.example.com" {
			t.Fatalf("invalid DNS cleanup evidence: %+v", a)
		}
	}
	if count != 2 {
		t.Fatalf("lost DNS value: %+v", artifacts)
	}
}

func TestComposerNukeRoleListsRejectMissingOrTruncatedOwnershipEvidence(t *testing.T) {
	for _, body := range []string{`{}`, `{"PolicyNames":[],"IsTruncated":true}`, `{"PolicyNames":null,"IsTruncated":false}`} {
		a := nukeStrictRoleAPI{base: nukeAdapterAWS(func(v any, args ...string) (bool, error) { return nukeDecode(v, body) })}
		var policies struct {
			PolicyNames []string
			IsTruncated bool
		}
		if _, e := a.Call(&policies, "iam", "list-role-policies", "--role-name", "role"); e == nil {
			t.Fatalf("incomplete ownership accepted: %s", body)
		}
	}
}

func TestComposerNukeSavedTargetInventoryFailureBlocksExternalCleanup(t *testing.T) {
	for _, credentialFailure := range []bool{false, true} {
		t.Run(itoaNuke(map[bool]int{false: 0, true: 1}[credentialFailure]), func(t *testing.T) {
			root := t.TempDir()
			scope := teardown.Scope{Application: "attestra", Account: "123456789012"}
			p := pulumicli.Project{Name: "attestra-application", Directory: filepath.Join(root, "infra"), Backend: "s3://saved-unreachable-state", Region: "us-east-2"}
			base, e := pulumicli.New(pulumicli.Config{Scope: scope, Projects: []pulumicli.Project{p}}, nukeAdapterRunner(func(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
				if name == "aws" && args[0] == "sts" {
					return []byte(`{"Account":"123456789012"}`), nil
				}
				return nil, errors.New("saved backend inaccessible")
			}))
			if e != nil {
				t.Fatal(e)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("external access after failed inventory: %s", r.Method)
				w.WriteHeader(500)
			}))
			defer server.Close()
			g := github.NewClient("test")
			g.BaseURL = server.URL
			d := &composerNukeDriver{Base: base, Account: scope.Account, Repository: "owner/repo", GitHub: g, AWSForRegion: nukeReadOnlyAWS(t)}
			if credentialFailure {
				d.BeforeInventory = func(teardown.Target) error { return errors.New("missing prior passphrase") }
			}
			target := teardown.Target{ID: p.Backend + "|" + p.Name + "|old", Project: p.Name, Stack: "old", Directory: p.Directory, Backend: p.Backend, Region: p.Region}
			if _, e = d.Inventory(context.Background(), target); e == nil {
				t.Fatal("unreachable saved target inventory succeeded")
			}
			if len(d.knownTargets) != 1 || d.knownTargets[0].ID != target.ID {
				t.Fatal("saved undiscovered target not tracked")
			}
			for _, kind := range []string{"attestra_github_environment", "attestra_deployment_role"} {
				a := teardown.Artifact{ID: "old", Kind: kind, Data: map[string]string{"account": d.Account, "repository": d.Repository, "environment": "old"}}
				if e = d.Cleanup(context.Background(), a); e == nil {
					t.Fatalf("%s cleanup allowed after failed saved inventory", kind)
				}
			}
		})
	}
}

func TestComposerNukeBeforeMutationRefusalStopsEveryMutationEntry(t *testing.T) {
	guarded := 0
	d := &composerNukeDriver{BeforeMutation: func() error { guarded++; return errors.New("deployment started") }, AWSForRegion: func(string) awsenv.API { t.Fatal("cloud probe after mutation guard refusal"); return nil }}
	calls := []func() error{func() error { return d.Destroy(context.Background(), teardown.Target{}) }, func() error { return d.Cleanup(context.Background(), teardown.Artifact{}) }, func() error { return d.PrepareRecovery(context.Background(), nil) }, func() error { return d.RemoveRecovery(context.Background(), teardown.Artifact{}) }}
	for _, call := range calls {
		if e := call(); e == nil {
			t.Fatal("failed activity guard ignored")
		}
	}
	if guarded != 4 {
		t.Fatalf("guard skipped: %d", guarded)
	}
}

func TestComposerNukeSavedDNSWithoutZoneCanResumeWithNewToken(t *testing.T) {
	zone, record := strings.Repeat("a", 32), strings.Repeat("b", 32)
	present, deletes := true, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/zones":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []map[string]string{{"id": zone, "name": "example.com", "status": "active"}}})
		case r.Method == "GET" && r.URL.Path == "/zones/"+zone+"/dns_records":
			if r.URL.Query().Get("name") != "api.example.com" {
				t.Errorf("new DNS target discovered: %s", r.URL.RawQuery)
			}
			records := []map[string]string{}
			if present {
				records = append(records, map[string]string{"id": record, "name": "api.example.com", "type": "CNAME", "content": "target.example.net"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": records})
		case r.Method == "DELETE" && r.URL.Path == "/zones/"+zone+"/dns_records/"+record:
			deletes++
			present = false
			_, _ = w.Write([]byte(`{"success":true,"result":{}}`))
		default:
			t.Errorf("unexpected DNS request: %s %s", r.Method, r.URL)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	cf := dns.NewClient("new-token")
	cf.BaseURL = server.URL
	d := &composerNukeDriver{Cloudflare: cf, Account: "123456789012", Repository: "owner/repo", AWSForRegion: nukeReadOnlyAWS(t)}
	a := teardown.Artifact{ID: "saved-name-type-content-id", Kind: "attestra_dns", Data: map[string]string{"account": d.Account, "repository": d.Repository, "name": "api.example.com", "type": "CNAME", "content": "target.example.net", "cleanup": "true"}}
	scope := teardown.Scope{Application: "attestra", Account: d.Account}
	store := &teardown.FileStore{Path: filepath.Join(t.TempDir(), "report.json")}
	if e := store.Save(&teardown.Report{Version: 1, Scope: scope, Artifacts: []teardown.Artifact{a}}); e != nil {
		t.Fatal(e)
	}
	engine := &teardown.Engine{Scope: scope, Driver: d, Store: store}
	if report, e := engine.Check(context.Background(), false); !errors.Is(e, teardown.ErrIncomplete) || len(report.Checks) != 1 || report.Checks[0].Status != teardown.Remains {
		t.Fatalf("saved zone-less DNS not verified: %+v %v", report, e)
	}
	if _, e := engine.Check(context.Background(), true); !errors.Is(e, teardown.ErrIncomplete) {
		t.Fatalf("fast check lost saved DNS: %v", e)
	}
	if deletes != 0 {
		t.Fatal("read-only checks mutated DNS")
	}
	if e := d.Cleanup(context.Background(), a); e != nil {
		t.Fatal(e)
	}
	if _, e := engine.Check(context.Background(), false); e != nil {
		t.Fatal(e)
	}
	if _, e := engine.Check(context.Background(), true); e != nil {
		t.Fatal(e)
	}
	if deletes != 1 || present {
		t.Fatalf("saved DNS cleanup failed: deletes=%d", deletes)
	}
	if a.Data["zone"] != "" {
		t.Fatal("read-back mutated saved artifact")
	}
}

func TestComposerNukeArtifactOnlyChecksFailClosedWithoutPulumiRoute(t *testing.T) {
	d := &composerNukeDriver{Account: "123456789012", Repository: "owner/repo", AWSForRegion: nukeReadOnlyAWS(t)}
	scope := teardown.Scope{Application: "attestra", Account: d.Account}
	for _, kind := range []string{"unsupported", "attestra_retained_key"} {
		artifact := teardown.Artifact{ID: "saved", Kind: kind, Data: map[string]string{"account": d.Account, "repository": d.Repository}}
		store := &teardown.FileStore{Path: filepath.Join(t.TempDir(), "report.json")}
		if e := store.Save(&teardown.Report{Version: 1, Scope: scope, Artifacts: []teardown.Artifact{artifact}}); e != nil {
			t.Fatal(e)
		}
		engine := &teardown.Engine{Scope: scope, Driver: d, Store: store}
		report, e := engine.Check(context.Background(), false)
		if !errors.Is(e, teardown.ErrIncomplete) || len(report.Checks) != 1 || report.Checks[0].Status != teardown.Unverified {
			t.Fatalf("missing route did not fail closed: %+v %v", report, e)
		}
	}
}
