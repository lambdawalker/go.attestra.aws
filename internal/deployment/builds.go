// Package deployment defines the application-owned Lambda entrypoints.
package deployment

import "github.com/lambdawalker/go.apexfission.aws.auth/core"

func SharedBuilds() []core.BuildTarget {
	return []core.BuildTarget{{Name: "admin", Package: "./cmd/admin"}, {Name: "index", Package: "./cmd/index"}}
}
func BackendBuilds() []core.BuildTarget {
	names := []string{"signup", "resend", "confirm", "challenge", "passkeyoptions", "passkeycomplete", "capture", "capture-worker", "capture-dispatcher", "app-config", "environment-reset"}
	targets := make([]core.BuildTarget, 0, len(names)+9)
	for _, name := range names {
		targets = append(targets, core.BuildTarget{Name: name, Package: "./cmd/" + name})
	}
	for _, name := range []string{"auth-email-start", "auth-email-complete", "auth-passkey-start", "auth-passkey-complete", "auth-refresh", "auth-status", "auth-password-start", "auth-password-complete", "auth-password-set"} {
		targets = append(targets, core.BuildTarget{Name: name, Package: "./cmd/signin"})
	}
	return targets
}
