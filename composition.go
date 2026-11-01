// Package application owns Attestra composition; feature code never imports it.
package application

import (
	"errors"
	"net/mail"
	"net/url"
	"strings"

	auth "github.com/lambdawalker/go.apexfission.aws.auth/feature"
	composer "github.com/lambdawalker/go.infra.composer"
	"github.com/lambdawalker/go.infra.composer/configuration"
)

const (
	AuthenticationID = "authentication"
	RuntimeID        = "application-runtime"
	RuntimeKind      = "attestra:runtime:Reference"
)

type RuntimeBinding struct {
	TableName, TableARN string
	TestOnly            bool
}

// New registers application-owned shared state and explicitly injects it into
// authentication. Additional independently authored features may consume its
// identity/API offerings without importing its implementation.
func New(runtime RuntimeBinding, additional ...*composer.Feature) (*composer.Application, error) {
	if strings.TrimSpace(runtime.TableName) == "" || strings.TrimSpace(runtime.TableARN) == "" {
		return nil, errors.New("shared runtime table name and ARN are required; deploy application control plane first")
	}
	for _, f := range additional {
		if f != nil {
			for _, r := range f.Resources {
				if r.Kind == auth.Kind {
					return nil, errors.New("the native authentication adapter supports one authentication backend per stack")
				}
			}
		}
	}
	shared := &composer.Feature{ID: RuntimeID, Source: composer.Origin(), Resources: []composer.Resource{{ID: "existing", Kind: RuntimeKind, Source: composer.Origin(), Inputs: map[string]composer.Value{"table_name": composer.Literal(runtime.TableName), "table_arn": composer.Literal(runtime.TableARN)}, Outputs: map[string]composer.Field{"table_name": {Type: composer.String}, "table_arn": {Type: composer.String}}}}, Offerings: []composer.Offering{{ID: "policy", Contract: auth.RuntimeContract(), TestOnly: runtime.TestOnly, Values: map[string]composer.Value{"table_name": composer.ResourceRef(RuntimeID+".existing", "table_name"), "table_arn": composer.ResourceRef(RuntimeID+".existing", "table_arn")}}}}
	features := append([]*composer.Feature{shared, auth.Declare(AuthenticationID)}, additional...)
	app := composer.NewApplication("attestra", features...)
	if err := app.Inject(AuthenticationID, auth.RuntimeRequirement, RuntimeID, "policy"); err != nil {
		return nil, err
	}
	return app, nil
}

type AuthSettings struct {
	AppOrigin, SenderAddress       string
	DiagnosticMode, CaptureEnabled bool
}

// AuthenticationConfiguration is an application policy decision, not a claim
// that declaring an identity provisions or verifies effective AWS access.
// Values seed deployment inputs; runtimeconfig remains authoritative for live
// onboarding overrides and its transactional grant/consent rules.
func AuthenticationConfiguration(environment string, settings AuthSettings) (*configuration.Plan, error) {
	origin, err := url.Parse(settings.AppOrigin)
	if err != nil || origin.Scheme != "https" || origin.Hostname() == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return nil, errors.New("app origin must be a credential-free HTTPS origin")
	}
	address, err := mail.ParseAddress(settings.SenderAddress)
	if err != nil || address.Address != settings.SenderAddress {
		return nil, errors.New("sender address must be a plain valid email address")
	}
	d := auth.ConfigurationContract(AuthenticationID, environment)
	values := map[string]composer.Value{"app_origin": composer.Literal(settings.AppOrigin), "sender_address": composer.Literal(settings.SenderAddress), "diagnostic_mode": composer.Literal(settings.DiagnosticMode), "capture_enabled": composer.Literal(settings.CaptureEnabled)}
	for _, p := range auth.ConfigurableProperties() {
		s := configuration.Source{Name: p.Name, Storage: "environment", Reader: p.Reader, Location: AuthenticationID + ".backend", Key: p.NativeKey, Scope: environment, ReaderIdentity: "authentication-runtime", WriterIdentity: "application-deployment", UpdateMethod: "lambda-configuration-rollout", Controls: []string{"scoped-access"}, CostKnown: true}
		b := configuration.Binding{Artifact: AuthenticationID + ".backend", Property: p.Name, OnMissing: configuration.MissingError, OnUnavailable: configuration.UnavailableError}
		if p.Name == "proof_key" {
			// This reader requests an immutable VersionId, not an indefinitely cached
			// latest value. Changing versions requires a deployment/recycle workflow.
			s.Storage = "secretsmanager"
			s.Location = "email-proof-key"
			s.SecretDelivery = true
			s.Controls = []string{"encrypted", "scoped-access"}
			s.UpdateMethod = "pinned-secret-version-and-lambda-rollout"
			s.MaxStalenessSeconds = 1
		} else {
			v := values[p.Name]
			b.Initial = &v
		}
		b.Sources = []configuration.Source{s}
		d.Bindings = append(d.Bindings, b)
	}
	return configuration.Validate(d)
}
