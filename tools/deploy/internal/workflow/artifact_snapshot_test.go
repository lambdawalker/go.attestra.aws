package workflow

import (
	"encoding/json"
	"errors"
	"github.com/lambdawalker/go.infra.composer/reporting"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type snapshotRunner struct {
	calls []string
	input []byte
	mode  os.FileMode
	fail  bool
}

func (r *snapshotRunner) Exec(dir string, capture bool, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	if name == "pulumi" {
		return []byte(`{"attestra-application:runtimeTableName":{"value":"runtime"},"attestra-application:runtimeTableArn":{"value":"arn:aws:dynamodb:us-east-2:123456789012:table/runtime"}}`), nil
	}
	for i, a := range args {
		if a == "-input" {
			r.input, _ = os.ReadFile(args[i+1])
			st, _ := os.Stat(args[i+1])
			r.mode = st.Mode().Perm()
		}
	}
	if r.fail {
		return nil, errors.New("private")
	}
	return nil, nil
}
func snapshotAttempt(t *testing.T, r *snapshotRunner) *artifactAttempt {
	now := time.Now().UTC()
	return &artifactAttempt{p: &processRunner{}, r: r, root: t.TempDir(), dir: "infra", stack: "dev", upExecuted: true, upAttempted: true, nativeChecked: true, state: []byte(`{}`), report: reporting.Report{SchemaVersion: 1, Scope: reporting.Scope{Application: "attestra", Environment: "dev", Stack: "dev", Project: "attestra-application", Repository: "owner/repo", Account: "123456789012", Region: "us-east-2", Backend: "s3://state"}, Metadata: reporting.Metadata{Operation: "deploy", Outcome: "failed", StartedAt: now.Add(-time.Minute), FinishedAt: now}, Artifacts: []reporting.Artifact{{Key: "urn:a", Observation: reporting.Observation{Status: reporting.Unknown, Reason: reporting.CheckFailed}}}}}
}
func TestSnapshotPublicationIsPrivateAndOnlyCompleteRealDeploy(t *testing.T) {
	r := &snapshotRunner{}
	a := snapshotAttempt(t, r)
	a.publishSnapshot()
	if len(r.input) == 0 || r.mode != 0600 || len(r.calls) != 2 {
		t.Fatal(r.calls, r.mode)
	}
	var decoded reporting.Report
	if json.Unmarshal(r.input, &decoded) != nil || decoded.Metadata.Outcome != "failed" {
		t.Fatal(string(r.input))
	}
	for _, call := range r.calls {
		if strings.Contains(call, "urn:a") || strings.Contains(call, "Bearer") {
			t.Fatal(call)
		}
	}
	for _, mutate := range []func(*artifactAttempt){func(a *artifactAttempt) { a.upExecuted = false }, func(a *artifactAttempt) { a.targeted = true }, func(a *artifactAttempt) { a.nativeChecked = false }, func(a *artifactAttempt) { a.state = nil }, func(a *artifactAttempt) { a.report.Metadata.Operation = "preview" }, func(a *artifactAttempt) { a.report.Scope.Project = "attestra-control-plane" }} {
		r := &snapshotRunner{}
		a := snapshotAttempt(t, r)
		mutate(a)
		a.publishSnapshot()
		if len(r.calls) > 0 {
			t.Fatal("published ineligible attempt", r.calls)
		}
	}
	r = &snapshotRunner{fail: true}
	a = snapshotAttempt(t, r)
	a.publishSnapshot()
	if !strings.Contains(strings.Join(a.report.Warnings, ","), "snapshot_publish_failed") {
		t.Fatal(a.report.Warnings)
	}
}
func TestSnapshotIAMExactEnvironmentAndValidatedTable(t *testing.T) {
	arn := "arn:aws:dynamodb:us-east-2:123456789012:table/runtime"
	p, e := snapshotPublicationPolicy(arn, "123456789012", "us-east-2", "dev")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(p)
	for _, text := range []string{arn, "dynamodb:PutItem", "ForAllValues:StringEquals", "dynamodb:LeadingKeys", "ENV#dev", "Null", "false"} {
		if !strings.Contains(string(b), text) {
			t.Fatal(string(b))
		}
	}
	for _, bad := range []string{"*", arn + "/*", strings.Replace(arn, "123456789012", "999999999999", 1), strings.Replace(arn, "us-east-2", "us-west-2", 1)} {
		if _, e := snapshotPublicationPolicy(bad, "123456789012", "us-east-2", "dev"); e == nil {
			t.Fatal(bad)
		}
	}
}
func TestSnapshotCodeInvalidatesIndexSourceHash(t *testing.T) {
	root := stateTestDir(t)
	for _, dir := range []string{"registry", "internal", "adminapi", "artifactsnapshot", "cmd/admin", "adminsetup", "cmd/admin-setup", "cmd/index", "infra-index"} {
		os.MkdirAll(filepath.Join(root, dir), 0755)
	}
	for _, name := range []string{"go.mod", "go.sum", "artifactsnapshot/snapshot.go"} {
		os.WriteFile(filepath.Join(root, name), []byte("one"), 0600)
	}
	before, e := indexSourceHash(root)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(root, "artifactsnapshot/snapshot.go"), []byte("two"), 0600)
	after, e := indexSourceHash(root)
	if e != nil || before == after {
		t.Fatal("snapshot code omitted from shared source hash", e)
	}
}

func TestSnapshotHelperReceivesOnlyIsolatedRunnerCredentials(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process stub")
	}
	dir := t.TempDir()
	stub := "#!/bin/sh\nprintf '%s|%s' \"${AWS_ACCESS_KEY_ID:-}\" \"${AWS_PROFILE:-}\"\n"
	if e := os.WriteFile(filepath.Join(dir, "go"), []byte(stub), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AWS_ACCESS_KEY_ID", "ambient-private")
	r := &processRunner{env: []string{"PATH=" + os.Getenv("PATH"), "AWS_ACCESS_KEY_ID=isolated", "AWS_PROFILE=attestra-refresh"}}
	b, e := r.Exec(dir, true, "go", "run", "./cmd/artifact-snapshot", "-input", "private-file")
	if e != nil || string(b) != "isolated|attestra-refresh" {
		t.Fatal(string(b), e)
	}
	b, e = r.Exec(dir, true, "go", "build", "./cmd/admin")
	if e != nil || string(b) != "|" {
		t.Fatal("build inherited cloud credentials", string(b), e)
	}
}
