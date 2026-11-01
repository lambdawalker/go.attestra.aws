package workflow

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	envregistry "github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/registry"

	awsenv "github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/aws"

	"github.com/lambdawalker/go.attestra.aws/tools/deploy/internal/github"
)

type verificationAWS struct {
	found    bool
	err      error
	response string
	calls    int
	args     []string
}

func (a *verificationAWS) Call(v any, args ...string) (bool, error) {
	a.calls++
	a.args = append([]string(nil), args...)
	if v != nil && a.response != "" {
		if e := json.Unmarshal([]byte(a.response), v); e != nil {
			return false, e
		}
	}
	return a.found, a.err
}

func TestTeardownCapturesInventoryWithoutSecrets(t *testing.T) {
	p := teardownProgress{}
	state := []byte(`{"deployment":{"resources":[{"type":"pulumi:pulumi:Stack"},{"type":"aws:lambda/function:Function","urn":"function","id":"qa-worker","outputs":{"environment":{"variables":{"SECRET":"never-save-this"}}}},{"type":"aws:lambda/permission:Permission","urn":"permission","id":"allow","outputs":{"function":"qa-worker"}}]}}`)
	if e := captureTeardownEvidence(state, &p); e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(p)
	if !p.InventoryCaptured || len(p.Inventory) != 2 || strings.Contains(string(b), "never-save-this") {
		t.Fatalf("inventory missing or unsafe: %s", b)
	}
}
func TestTeardownVerificationRejectsPresentAndDeniedResources(t *testing.T) {
	probe := teardownProbe{Name: "worker", Kind: "lambda", ID: "qa-worker"}
	for _, tc := range []struct {
		name  string
		found bool
		err   error
		ok    bool
	}{
		{"present", true, nil, false}, {"absent", false, &awsenv.Error{Code: "ResourceNotFoundException"}, true}, {"denied", false, &awsenv.Error{Code: "AccessDeniedException"}, false}, {"network", false, errors.New("network"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &verificationAWS{found: tc.found, err: tc.err}
			e := verifyTeardownProbe(a, probe)
			if (e == nil) != tc.ok {
				t.Fatal(e)
			}
		})
	}
}
func TestTeardownVerificationRejectsUnknownAndMalformedEvidence(t *testing.T) {
	for _, p := range []teardownProbe{{Kind: "unknown", ID: "x"}, {Kind: "lambda"}} {
		a := &verificationAWS{}
		if verifyTeardownProbe(a, p) == nil || a.calls != 0 {
			t.Fatal("unsupported evidence accepted")
		}
	}
}
func TestTeardownListVerificationNeedsExplicitEmptyResult(t *testing.T) {
	for _, raw := range []string{`{}`, `{"VerificationAttributes":{"qa.example.com":{}}}`} {
		if verifyTeardownProbe(&verificationAWS{found: true, response: raw}, teardownProbe{Kind: "ses", ID: "qa.example.com"}) == nil {
			t.Fatal("accepted", raw)
		}
	}
	if e := verifyTeardownProbe(&verificationAWS{found: true, response: `{"VerificationAttributes":{}}`}, teardownProbe{Kind: "ses", ID: "qa.example.com"}); e != nil {
		t.Fatal(e)
	}
}
func TestTeardownFinalReportFailsClosed(t *testing.T) {
	for _, status := range []string{"REMAINS", "FAILED"} {
		if (teardownReport{{Status: status}}).Err() == nil {
			t.Fatal(status)
		}
	}
	if (teardownReport{{Status: "DELETED"}, {Status: "RETAINED"}, {Status: "SKIPPED"}}).Err() != nil {
		t.Fatal("approved exceptions should be reported separately")
	}
}

func TestTeardownRoute53UsesAPIInputForPagination(t *testing.T) {
	a := &verificationAWS{found: true, response: `{"ResourceRecordSets":[]}`}
	if e := verifyTeardownProbe(a, teardownProbe{Kind: "record", ID: "qa.example.com", Zone: "Z123", RecordType: "CNAME"}); e != nil {
		t.Fatal(e)
	}
	found := false
	for i, arg := range a.args {
		if arg == "--cli-input-json" {
			var recordInput map[string]string
			if json.Unmarshal([]byte(a.args[i+1]), &recordInput) != nil || recordInput["StartRecordName"] != "qa.example.com" || recordInput["MaxItems"] != "1" {
				t.Fatal(a.args)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("Route53 paginator hides start-record flags; use API JSON input", a.args)
	}
}
func TestTeardownIndexReadback(t *testing.T) {
	for _, body := range []string{`{"schemaVersion":1,"environments":[]}`, `{}`, `{"schemaVersion":1,"environments":[{"id":"qa"}]}`, `{"schemaVersion":1,"environments":null}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		e := verifyTeardownIndex(&envregistry.Client{URL: server.URL, Environment: "qa", HTTP: server.Client()})
		server.Close()
		if (e == nil) != (body == `{"schemaVersion":1,"environments":[]}`) {
			t.Fatal(body, e)
		}
	}
}

type teardownVerifyRunner struct {
	body string
	err  error
}

func (r teardownVerifyRunner) Exec(string, bool, string, ...string) ([]byte, error) {
	return []byte(r.body), r.err
}
func TestTeardownValidationChecksLocalFilesAndLegacyInventory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer server.Close()
	g := github.NewClient("test")
	g.BaseURL = server.URL
	root := t.TempDir()
	p := teardownProgress{Environment: "qa", Repository: "owner/repo", Values: map[string]string{"AWS_ROLE_ARN": "arn:aws:iam::123:role/qa"}}
	verify := func() teardownReport {
		return verifyTeardown(root, &p, &verificationAWS{}, teardownVerifyRunner{body: "[]"}, g, nil, nil)
	}
	if verify().Err() == nil {
		t.Fatal("legacy missing inventory passed")
	}
	p.InventoryCaptured = true
	if e := verify().Err(); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Join(root, "android-config"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "android-config", "qa.properties"), []byte("api"), 0600); e != nil {
		t.Fatal(e)
	}
	if verify().Err() == nil {
		t.Fatal("remaining Android export passed")
	}
	os.Remove(filepath.Join(root, "android-config", "qa.properties"))
	os.MkdirAll(filepath.Join(root, "infra"), 0700)
	os.WriteFile(filepath.Join(root, "infra", "Pulumi.qa.yaml"), []byte("config: {}"), 0600)
	if verify().Err() == nil {
		t.Fatal("remaining stack YAML passed")
	}

}
func TestTeardownValidationRetriesOnlyRemainingAndPersistsFailure(t *testing.T) {
	for _, status := range []string{"REMAINS", "FAILED", "DELETED"} {
		p := teardownProgress{}
		calls, saves, waits := 0, 0, 0
		err := finalizeTeardownValidation(&p, func() error { saves++; return nil }, func() teardownReport { calls++; return teardownReport{{Name: "resource", Status: status}} }, func(time.Duration) { waits++ })
		expected := 1
		if status == "REMAINS" {
			expected = 3
		}
		if calls != expected || saves != expected || waits != expected-1 || p.ValidatedAt == "" || p.Complete || (err == nil) != (status == "DELETED") {
			t.Fatal(status, calls, saves, waits, err)
		}
	}
}

func TestTeardownInventoryMergeDoesNotCertifyLegacyCheckpoint(t *testing.T) {
	p := teardownProgress{}
	if e := captureTeardownInventory([]byte(`{"deployment":{"resources":[]}}`), &p); e != nil {
		t.Fatal(e)
	}
	if p.InventoryCaptured {
		t.Fatal("remaining export cannot certify original legacy inventory")
	}
}
func TestTeardownInventoryRequiresResourceArray(t *testing.T) {
	for _, raw := range []string{`{"deployment":{}}`, `{"deployment":{"resources":null}}`} {
		if captureTeardownInventory([]byte(raw), &teardownProgress{}) == nil {
			t.Fatal("accepted malformed export", raw)
		}
	}
}
func TestTeardownRoute53RejectsMalformedRecords(t *testing.T) {
	if verifyTeardownProbe(&verificationAWS{found: true, response: `{"ResourceRecordSets":[{}]}`}, teardownProbe{Kind: "record", ID: "qa.example.com", Zone: "Z123", RecordType: "CNAME"}) == nil {
		t.Fatal("malformed record accepted")
	}
}

func TestTeardownInventorySkipsExportedComponentsButKeepsCustomAndLegacyResources(t *testing.T) {
	p := teardownProgress{}
	raw := `{"deployment":{"resources":[{"type":"attestra:runtime:Reference","urn":"reference","custom":false},{"type":"aws:lambda/function:Function","urn":"custom","id":"worker","custom":true},{"type":"aws:lambda/function:Function","urn":"legacy","id":"old-worker"},{"type":"aws:secretsmanager/secret:Secret","urn":"secret","id":"arn:aws:secretsmanager:us-east-2:123:secret:email-proof-key-abc","custom":true},{"type":"aws:secretsmanager/secretVersion:SecretVersion","urn":"version","id":"arn:aws:secretsmanager:us-east-2:123:secret:email-proof-key-abc|version-1","custom":true,"outputs":{"secretString":"NEVER-PERSIST"}}]}}`
	if err := captureTeardownInventory([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Inventory) != 4 {
		t.Fatalf("component or real resources mishandled: %+v", p.Inventory)
	}
	if p.Inventory[2].Kind != "secret" || p.Inventory[3].Kind != "secret-version" {
		t.Fatalf("missing secret metadata probes: %+v", p.Inventory)
	}
	body, _ := json.Marshal(p)
	if strings.Contains(string(body), "NEVER-PERSIST") {
		t.Fatal("secret material persisted")
	}
}

type secretMetadataAWS struct {
	describe, versions string
	err                error
	calls              [][]string
}

func (a *secretMetadataAWS) Call(v any, args ...string) (bool, error) {
	a.calls = append(a.calls, append([]string(nil), args...))
	if a.err != nil {
		return false, a.err
	}
	body := a.describe
	if len(args) > 1 && args[1] == "list-secret-version-ids" {
		body = a.versions
	}
	if json.Unmarshal([]byte(body), v) != nil {
		return false, errors.New("invalid fake response")
	}
	return true, nil
}
func TestTeardownSecretsUseOnlyMetadataAndFailClosed(t *testing.T) {
	arn := "arn:aws:secretsmanager:us-east-2:123:secret:email-proof-key-abc"
	for _, kind := range []string{"secret", "secret-version"} {
		id := arn
		if kind == "secret-version" {
			id += "|version-1"
		}
		for _, tc := range []struct {
			name, describe, versions string
			err                      error
			absent, pending          bool
		}{
			{name: "absent", err: &awsenv.Error{Code: "ResourceNotFoundException"}, absent: true},
			{name: "denied", err: &awsenv.Error{Code: "AccessDeniedException"}},
			{name: "invalid request", err: &awsenv.Error{Code: "InvalidRequestException"}},
			{name: "transport", err: errors.New("network")},
			{name: "present", describe: `{"ARN":"` + arn + `"}`, versions: `{"Versions":[{"VersionId":"version-1"}]}`},
			{name: "version absent", describe: `{"ARN":"` + arn + `"}`, versions: `{"Versions":[]}`, absent: kind == "secret-version"},
			{name: "pending recovery", describe: `{"ARN":"` + arn + `","DeletedDate":"2026-12-01T00:00:00Z"}`, pending: true},
			{name: "malformed metadata", describe: `{}`},
			{name: "overflowing deadline", describe: `{"ARN":"` + arn + `","DeletedDate":1e100}`},
			{name: "malformed deadline", describe: `{"ARN":"` + arn + `","DeletedDate":"invalid"}`},
			{name: "malformed versions", describe: `{"ARN":"` + arn + `"}`, versions: `{}`},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				a := &secretMetadataAWS{describe: tc.describe, versions: tc.versions, err: tc.err}
				err := verifyTeardownProbe(a, teardownProbe{Kind: kind, ID: id})
				if (err == nil) != tc.absent {
					t.Fatalf("absence=%v: %v", tc.absent, err)
				}
				if tc.pending && (err == nil || !strings.Contains(err.Error(), "recoverable") || !strings.Contains(err.Error(), "2026-12-01")) {
					t.Fatalf("pending recovery deadline not reported: %v", err)
				}
				for _, call := range a.calls {
					if call[0] != "secretsmanager" || (call[1] != "describe-secret" && call[1] != "list-secret-version-ids") {
						t.Fatalf("not metadata-only: %v", call)
					}
					if !strings.Contains(strings.Join(call, " "), "--secret-id "+arn) {
						t.Fatalf("provider version ID not split: %v", call)
					}
				}
			})
		}
	}
}
func TestTeardownPendingSecretIsReportedAsRetainedWithDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer server.Close()
	g := github.NewClient("test")
	g.BaseURL = server.URL
	arn := "arn:aws:secretsmanager:us-east-2:123:secret:email-proof-key-abc"
	p := teardownProgress{Environment: "qa", Repository: "owner/repo", InventoryCaptured: true, Values: map[string]string{"AWS_ROLE_ARN": "arn:aws:iam::123:role/qa"}, Inventory: []teardownProbe{{Name: "proof", Kind: "secret", ID: arn}}}
	a := &verificationAWS{found: true, response: `{"ARN":"` + arn + `","DeletedDate":1796083200}`, err: nil}
	report := verifyTeardown(t.TempDir(), &p, a, teardownVerifyRunner{body: "[]"}, g, nil, nil)
	if report[0].Status != "RETAINED" || !strings.Contains(report[0].Detail, "recoverable") || !strings.Contains(report[0].Detail, "2026-12-01") {
		t.Fatal(report[0])
	}
}
