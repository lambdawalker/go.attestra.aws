package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lambdawalker/go.attestra.aws/artifactsnapshot"
)

// Table identity is obtained from setup's shared outputs, never a wildcard.
func validateSnapshotTable(arn, account, region string) (string, error) {
	prefix := "arn:aws:dynamodb:" + region + ":" + account + ":table/"
	if !regexp.MustCompile(`^[0-9]{12}$`).MatchString(account) || !regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-[0-9]+$`).MatchString(region) || !strings.HasPrefix(arn, prefix) {
		return "", errors.New("invalid snapshot runtime table")
	}
	name := strings.TrimPrefix(arn, prefix)
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]{3,255}$`).MatchString(name) {
		return "", errors.New("invalid snapshot runtime table")
	}
	return name, nil
}
func snapshotPublicationPolicy(arn, account, region, environment string) (map[string]any, error) {
	if _, e := validateSnapshotTable(arn, account, region); e != nil {
		return nil, e
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,62}$`).MatchString(environment) {
		return nil, errors.New("invalid snapshot environment")
	}
	return map[string]any{"Version": "2012-10-17", "Statement": []any{map[string]any{"Effect": "Allow", "Action": "dynamodb:PutItem", "Resource": arn, "Condition": map[string]any{"ForAllValues:StringEquals": map[string][]string{"dynamodb:LeadingKeys": {"ENV#" + environment}}, "Null": map[string]string{"dynamodb:LeadingKeys": "false"}}}}}, nil
}
func (a *artifactAttempt) publishSnapshot() {
	// Partial, preflight-only, cached and preview attempts cannot supersede a
	// complete deployment snapshot. Failed real deployments retain truthful data.
	if a.report.Scope.Project != "attestra-application" || a.report.Metadata.Operation != "deploy" || !a.upExecuted || a.targeted || !a.nativeChecked || len(a.state) == 0 {
		return
	}
	fail := func() {
		a.warn("snapshot_publish_failed")
		fmt.Fprintln(os.Stderr, "Artifact snapshot publication failed; local reports remain available.")
	}
	if _, e := artifactsnapshot.Encode(a.report, a.stack); e != nil {
		fail()
		return
	}
	b, e := a.r.Exec(a.dir, true, "pulumi", "config", "--json", "--stack", a.stack)
	if e != nil {
		fail()
		return
	}
	var cfg map[string]struct{ Value string }
	if json.Unmarshal(b, &cfg) != nil {
		fail()
		return
	}
	name, e := validateSnapshotTable(cfg["attestra-application:runtimeTableArn"].Value, a.report.Scope.Account, a.report.Scope.Region)
	if e != nil || name != cfg["attestra-application:runtimeTableName"].Value {
		fail()
		return
	}
	// A private directory also protects the input path on platforms whose file
	// modes are implemented through inherited directory ACLs.
	dir, e := os.MkdirTemp("", "attestra-snapshot-")
	if e != nil {
		fail()
		return
	}
	defer os.RemoveAll(dir)
	input := filepath.Join(dir, "report.json")
	b, e = json.Marshal(a.report)
	if e != nil || os.WriteFile(input, b, 0600) != nil {
		fail()
		return
	}
	if _, e = a.r.Exec(a.root, true, "go", "run", "./cmd/artifact-snapshot", "-table", name, "-environment", a.stack, "-input", input); e != nil {
		fail()
	}
}
