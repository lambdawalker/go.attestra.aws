package workflow

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestTimedPulumiApprovals(t *testing.T) {
	for _, tc := range []struct {
		name         string
		args         []string
		enabled, ask bool
	}{
		{"pulumi", []string{"up", "--stack", "qa", "--target", "urn:target"}, true, true},
		{"pulumi", []string{"up", "--stack", "shared"}, true, true},
		{"pulumi", []string{"up", "--stack", "dev"}, false, false},
		{"pulumi", []string{"up", "--yes"}, true, false},
		{"pulumi", []string{"preview"}, true, false},
		{"pulumi", []string{"destroy"}, true, false},
		{"go", []string{"run", ".", "-build"}, true, false},
	} {
		original := append([]string(nil), tc.args...)
		called := false
		got, err := timedPulumiArgs(tc.name, tc.args, tc.enabled, func(title string) error {
			called = true
			if !strings.Contains(title, "Apply") {
				t.Fatal(title)
			}
			return nil
		})
		if err != nil || called != tc.ask {
			t.Fatal(got, err, called)
		}
		want := original
		if tc.ask {
			want = append(append([]string(nil), original...), "--yes", "--non-interactive")
		}
		if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(original, tc.args) {
			t.Fatal(got, want, tc.args)
		}
	}
	stopped := errors.New("cancelled")
	if args, err := timedPulumiArgs("pulumi", []string{"up", "--stack", "dev"}, true, func(string) error { return stopped }); !errors.Is(err, stopped) || args != nil {
		t.Fatal("cancel did not stop", args, err)
	}
}

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
