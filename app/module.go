// Package app is Attestra's application-owned extension of the auth framework.
package app

import (
	"context"
	"embed"
	"encoding/json"
	"github.com/lambdawalker/go.apexfission.aws.auth/admin"
	"github.com/lambdawalker/go.apexfission.aws.auth/application"
	"github.com/lambdawalker/go.apexfission.aws.auth/auth"
	"github.com/lambdawalker/go.apexfission.aws.auth/authorization"
	"github.com/lambdawalker/go.apexfission.aws.auth/core"
	"github.com/lambdawalker/go.apexfission.aws.auth/router"
	"net/http"
)

//go:embed web/*
var assets embed.FS

type Module struct{}

func (Module) Describe() core.Descriptor {
	return core.Descriptor{ID: "attestra", Version: "0.1.0", Dependencies: []core.Dependency{{ID: "auth", Major: 0}}}
}
func (Module) Permissions() []string { return []string{"attestra:profile", "attestra:admin"} }
func (Module) BuildTargets() []core.BuildTarget {
	return []core.BuildTarget{{Name: "application", Package: "./cmd/api"}, {Name: "challenge", Package: "./cmd/challenge"}}
}
func (Module) AdminPages() []admin.Page {
	return []admin.Page{{ID: "attestra", Title: "Attestra", Permission: "attestra:admin", Entry: "web/index.html", Assets: assets}}
}
func (Module) Routes() []router.Route {
	return []router.Route{{Method: "GET", Path: "/health", Lambda: "application", Access: router.Public, Handler: http.HandlerFunc(health)}, {Method: "GET", Path: "/v1/me", Lambda: "application", Permission: "attestra:profile", Handler: http.HandlerFunc(profile)}, {Method: "GET", Path: "/admin/api/extensions/attestra/status", Lambda: "admin", Admin: true, Permission: "attestra:admin", Handler: http.HandlerFunc(health)}}
}

// Compose is shared by runtime, build discovery and infrastructure registration.
func Compose(a auth.Module) (*application.Application, error) {
	a.Lambda = "application"
	return application.Compose(a, Module{})
}
func health(w http.ResponseWriter, _ *http.Request) {
	reply(w, map[string]string{"application": "attestra", "status": "ok"})
}
func profile(w http.ResponseWriter, r *http.Request) {
	p, ok := authorization.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", 401)
		return
	}
	reply(w, map[string]string{"subject": p.Subject})
}
func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// ProfileAuthorizer grants only access to the verified caller's own identity.
// Admin requests use a separate issuer and DynamoGrants instead.
type ProfileAuthorizer struct{}

func (ProfileAuthorizer) Authorize(_ context.Context, p authorization.Principal, _, permission string) error {
	if p.Subject != "" && permission == "attestra:profile" {
		return nil
	}
	return authorization.ErrDenied
}
