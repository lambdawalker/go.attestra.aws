package workflow

import (
	"encoding/json"
	"github.com/lambdawalker/go.infra.composer/ownership"
	"strings"
	"testing"
)

type tagAPI struct {
	calls    [][]string
	existing []struct{ Key, Value string }
}

func (f *tagAPI) Call(result any, args ...string) (bool, error) {
	f.calls = append(f.calls, append([]string{}, args...))
	if result != nil {
		b, _ := json.Marshal(map[string]any{"TagSet": f.existing})
		json.Unmarshal(b, result)
	}
	return true, nil
}
func TestStateOwnershipPreservesTagsAndRejectsForeignProject(t *testing.T) {
	a := &tagAPI{existing: []struct{ Key, Value string }{{"team", "platform"}}}
	m := ownership.Metadata{Application: "attestra", Environment: "shared", Domain: "attestrabond.com", Feature: "deployment", Deployment: "attestra-state"}
	if err := labelStateBucket(a, testOptions(), "123456789012", m); err != nil {
		t.Fatal(err)
	}
	text := ""
	for _, c := range a.calls {
		text += strings.Join(c, " ")
	}
	if !strings.Contains(text, "platform") || !strings.Contains(text, "ic:environment") || !strings.Contains(text, "shared") {
		t.Fatal("state tags replaced or incomplete", text)
	}
	a = &tagAPI{existing: []struct{ Key, Value string }{{"ic:application", "another-app"}}}
	if err := labelStateBucket(a, testOptions(), "123456789012", m); err == nil {
		t.Fatal("foreign ownership overwritten")
	}
	if len(a.calls) != 1 {
		t.Fatal("wrote after conflict")
	}
}
func TestOwnershipIAMIncludesMutableDescriptions(t *testing.T) {
	text := policyJSON(deploymentPolicies("123456789012", testOptions()))
	for _, required := range []string{"iam:UpdateRoleDescription"} {
		if !strings.Contains(text, required) {
			t.Fatal("ownership deployment permission missing", required)
		}
	}
}

func TestRoleOwnershipIsVerifiedAndIdempotent(t *testing.T) {
	f := &setupFake{subject: "repo:owner/repo:environment:dev", environment: "dev", provider: true, audience: true, role: true, owned: true, policies: map[string]any{}}
	f.extraTags = map[string]string{"team": `platform,security "owners"`}
	f.trust = githubTrust("123456789012", f.subject)
	plan, err := inspectAWSSetup(f, "123456789012", "attestra-github-deploy", f.subject, "dev", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	meta := ownership.Metadata{Application: "attestra", Environment: "dev", Domain: "app.example.test", Feature: "deployment", Deployment: "attestra-dev"}
	plan.Ownership = &meta
	if _, err := applyAWSSetup(f, plan); err != nil {
		t.Fatal(err)
	}
	if f.extraTags["team"] != `platform,security "owners"` {
		t.Fatal("existing tag punctuation corrupted")
	}
	if f.extraTags["ic:environment"] != "dev" {
		t.Fatal("role not labeled")
	}
	f.writes = nil
	plan, err = inspectAWSSetup(f, "123456789012", "attestra-github-deploy", f.subject, "dev", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	plan.Ownership = &meta
	if _, err := applyAWSSetup(f, plan); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 0 {
		t.Fatal("unchanged tags were rewritten", f.writes)
	}
	f.extraTags["ic:application"] = "foreign"
	plan, err = inspectAWSSetup(f, "123456789012", "attestra-github-deploy", f.subject, "dev", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	plan.Ownership = &meta
	if _, err := applyAWSSetup(f, plan); err == nil {
		t.Fatal("foreign owner accepted")
	}
	if len(f.writes) != 0 {
		t.Fatal("conflict mutated IAM")
	}
}
