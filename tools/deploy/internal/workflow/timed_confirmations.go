package workflow

import (
	"fmt"
	"slices"
)

func validateTimedConfirmationMode(enabled bool, flags []string) error {
	if !enabled {
		return nil
	}
	for _, name := range flags {
		switch name {
		case "teardown", "teardown-index", "nuke", "check", "fast-check", "migrate-from", "ci", "associations", "manage-credentials", "build", "health-check", "export-android", "publish-index", "release-index-lock":
			return fmt.Errorf("-timed-confirm cannot be combined with -%s; timed approvals are for interactive setup and deployment", name)
		}
	}
	return nil
}

// Pulumi owns the final approval by default. In timed mode we replace that
// prompt, then let Pulumi run normally with an explicit approval. Already
// approved wizard steps retain their existing behavior.
func timedPulumiArgs(name string, args []string, enabled bool, confirm func(string) error) ([]string, error) {
	if !enabled || name != "pulumi" || len(args) == 0 || args[0] != "up" || slices.Contains(args, "--yes") || slices.Contains(args, "-y") || slices.Contains(args, "--yes=true") {
		return args, nil
	}
	target := "selected stack"
	for i := 1; i+1 < len(args); i++ {
		if args[i] == "--stack" {
			target = args[i+1]
			break
		}
	}
	if err := confirm("Apply the previewed Pulumi changes to " + target + "?"); err != nil {
		return nil, err
	}
	return append(slices.Clone(args), "--yes", "--non-interactive"), nil
}
