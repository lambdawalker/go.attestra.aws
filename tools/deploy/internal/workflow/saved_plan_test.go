package workflow

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func sealedTestPlan(t *testing.T) (string, string, *savedPlan) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "infra")
	os.Mkdir(dir, 0700)
	os.WriteFile(filepath.Join(root, "source.go"), []byte("source"), 0600)
	plan, e := newSavedPlan(root, dir, []string{"AWS_REGION=us-east-2"}, []string{"preview", "--stack", "dev"})
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(plan.path, []byte(`{"resourcePlans":{}}`), 0600)
	events := filepath.Join(filepath.Dir(plan.path), "events.json")
	os.WriteFile(events, []byte(`{"summaryEvent":{"resourceChanges":{"create":1,"same":2}}}`), 0600)
	if e = plan.seal(events); e != nil {
		t.Fatal(e)
	}
	return root, dir, plan
}
func TestSavedPlanBinding(t *testing.T) {
	root, dir, plan := sealedTestPlan(t)
	args := []string{"up", "--stack", "dev", "--yes", "--non-interactive"}
	env := []string{"AWS_REGION=us-east-2"}
	if e := plan.validate(root, dir, env, args); e != nil {
		t.Fatal(e)
	}
	if plan.destructive {
		t.Fatal("safe summary marked destructive")
	}
	savedTime := plan.sealedAt
	plan.sealedAt = time.Now().Add(-21 * time.Minute)
	if plan.validate(root, dir, env, args) == nil {
		t.Fatal("expired preview accepted")
	}
	plan.sealedAt = savedTime
	for _, change := range []struct {
		dir       string
		env, args []string
	}{{dir, env, []string{"up", "--stack", "prod"}}, {dir, []string{"AWS_REGION=us-west-2"}, args}, {dir, append(env, "AWS_ACCESS_KEY_ID=changed"), args}, {dir, env, append(args, "--target", "other")}, {filepath.Join(root, "infra-index"), env, args}} {
		if plan.validate(root, change.dir, change.env, change.args) == nil {
			t.Fatal("changed target accepted")
		}
	}
	os.WriteFile(filepath.Join(root, "source.go"), []byte("changed"), 0600)
	if plan.validate(root, dir, env, args) == nil {
		t.Fatal("stale source accepted")
	}
	root, dir, plan = sealedTestPlan(t)
	os.WriteFile(plan.path, []byte("tampered"), 0600)
	if plan.validate(root, dir, env, args) == nil {
		t.Fatal("tampered plan accepted")
	}
	if (*savedPlan)(nil).validate(root, dir, env, args) == nil {
		t.Fatal("missing preview accepted")
	}
}
func TestDestructiveOrUnknownPreviewCannotUseCIApproval(t *testing.T) {
	for _, events := range []string{`{"summaryEvent":{"resourceChanges":{"delete":1}}}`, `{"summaryEvent":{"resourceChanges":{"replace":1}}}`, `{"summaryEvent":{"resourceChanges":{"delete-replaced":1}}}`, `bad`, ``} {
		_, _, plan := sealedTestPlan(t)
		path := filepath.Join(filepath.Dir(plan.path), "events")
		os.WriteFile(path, []byte(events), 0600)
		if e := plan.seal(path); e != nil {
			t.Fatal(e)
		}
		if approveSavedPlan(plan, true) == nil {
			t.Fatal("CI approved destructive/unknown preview")
		}
	}
}
func TestExpectedAccountGuard(t *testing.T) {
	if verifyDeployAccount(options{ExpectedAccount: "123456789012"}, "123456789012", "123456789012") != nil {
		t.Fatal("matching identity refused")
	}
	for _, o := range []options{{ExpectedAccount: "999999999999"}, {}} {
		if verifyDeployAccount(o, "123456789012", "999999999998") == nil {
			t.Fatal("changed account accepted")
		}
	}
	if requireCIInputs([]string{"ci", "account", "stack", "backend", "region"}) != nil {
		t.Fatal("complete inputs refused")
	}
	if requireCIInputs([]string{"ci", "account"}) == nil {
		t.Fatal("implicit CI target accepted")
	}
}
func TestSetupApplyGuardRunsBeforeCommandOrApproval(t *testing.T) {
	p := &processRunner{setupOnly: true, reportDisabled: true}
	if _, e := p.Exec(filepath.Join(t.TempDir(), "infra-index"), true, "pulumi", "up", "--stack", "shared", "--yes"); e == nil {
		t.Fatal("setup escalated to apply")
	}
}

func TestProcessRunnerAppliesOnlySavedPlan(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Pulumi; Windows build is checked separately")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "infra-index")
	os.Mkdir(dir, 0700)
	bin := t.TempDir()
	trace := filepath.Join(root, ".deploy", "apply-args")
	script := `#!/bin/sh
op="$1"
shift
while [ "$#" -gt 0 ]; do
 case "$1" in
 --save-plan) shift; plan="$1";;
 --event-log) shift; log="$1";;
 --plan) shift; applied="$1";;
 --refresh) refreshed=yes;;
 --skip-preview) skipped=yes;;
 esac
 shift
done
case "$op" in
 preview) [ "$refreshed" = yes ] || exit 9; printf '{}' > "$plan"; printf '%s\n' '{"summaryEvent":{"resourceChanges":{"create":1}}}' > "$log";;
 up) [ "$APPLY_TEST_FAIL" != yes ] || exit 11; [ "$skipped" = yes ] || exit 10; [ -n "$applied" ] && [ -f "$applied" ] || exit 8; printf '%s' "$applied" > "$APPLY_TEST_TRACE";;
esac
`
	os.WriteFile(filepath.Join(bin, "pulumi"), []byte(script), 0700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("APPLY_TEST_TRACE", trace)
	p := &processRunner{ciApproved: true, reportDisabled: true}
	if _, e := p.Exec(dir, true, "pulumi", "up", "--stack", "dev", "--yes"); e == nil {
		t.Fatal("apply without preview allowed")
	}
	if _, e := p.Exec(dir, true, "pulumi", "preview", "--stack", "dev"); e != nil {
		t.Fatal(e)
	}
	planned := p.plans[dir].path
	if _, e := p.Exec(dir, true, "pulumi", "up", "--stack", "dev", "--yes"); e != nil {
		t.Fatal(e)
	}
	actual, e := os.ReadFile(trace)
	if e != nil || string(actual) != planned {
		t.Fatal("apply did not consume exact plan", e)
	}
	if _, e = os.Stat(planned); !os.IsNotExist(e) {
		t.Fatal("consumed plan retained")
	}
	if _, e := p.Exec(dir, true, "pulumi", "up", "--stack", "dev", "--yes"); e == nil {
		t.Fatal("approval replay allowed")
	}
	if _, e := p.Exec(dir, true, "pulumi", "preview", "--stack", "dev"); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(root, "changed.go"), []byte("changed"), 0600)
	if _, e := p.Exec(dir, true, "pulumi", "up", "--stack", "dev", "--yes"); e == nil {
		t.Fatal("changed inputs applied")
	}
	receipt := filepath.Join(root, ".deploy", "applied.attestra-control-plane.dev.local.json")
	before, e := os.ReadFile(receipt)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(dir, true, "pulumi", "preview", "--stack", "dev"); e != nil {
		t.Fatal(e)
	}
	t.Setenv("APPLY_TEST_FAIL", "yes")
	if _, e = p.Exec(dir, true, "pulumi", "up", "--stack", "dev", "--yes"); e == nil {
		t.Fatal("failed apply accepted")
	}
	after, e := os.ReadFile(receipt)
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("failed apply replaced success receipt", e)
	}
}
