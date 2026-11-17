package workflow

import (
	"fmt"
)

func validateTimedConfirmationMode(enabled bool, flags []string) error {
	if !enabled {
		return nil
	}
	for _, name := range flags {
		switch name {
		case "migrate-records", "migrate-vault", "teardown", "teardown-index", "nuke", "check", "fast-check", "migrate-from", "ci", "associations", "manage-credentials", "build", "health-check", "export-android", "publish-index", "release-index-lock":
			return fmt.Errorf("-timed-confirm cannot be combined with -%s; timed approvals are for interactive setup and deployment", name)
		}
	}
	return nil
}

// Credential, teardown and recovery operations retain deliberate interaction.
func interactiveDefaults(flags []string) bool {
	for _, name := range flags {
		switch name {
		case "migrate-records", "migrate-vault", "teardown", "teardown-index", "nuke", "check", "fast-check", "migrate-from", "ci", "associations", "manage-credentials", "build", "health-check", "export-android", "publish-index", "release-index-lock":
			return false
		}
	}
	return true
}
