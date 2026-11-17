package workflow

import (
	"testing"
)

func TestTimedModeOnlyAppliesToDeployment(t *testing.T) {
	for _, flag := range []string{"teardown", "teardown-index", "nuke", "check", "fast-check", "migrate-from", "ci", "associations", "manage-credentials", "build", "health-check", "export-android", "publish-index", "release-index-lock"} {
		if validateTimedConfirmationMode(true, []string{flag}) == nil {
			t.Fatal("allowed", flag)
		}
	}
	for _, flags := range [][]string{{"timed-confirm"}, {"scope", "timed-confirm"}, {"bootstrap", "timed-confirm"}, {"setup-github", "timed-confirm"}} {
		if err := validateTimedConfirmationMode(true, flags); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTimedModePreservesStartupChoice(t *testing.T) {
	root := t.TempDir()
	choice, err := deploymentStartup(root, "o/r", []string{"repo-root", "timed-confirm"}, func() (string, error) { t.Fatal("unexpected menu before setup"); return "", nil })
	if err != nil || choice != "setup" {
		t.Fatal(choice, err)
	}
}
