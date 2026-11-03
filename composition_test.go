package application

import (
	"errors"
	"strings"
	"testing"

	auth "github.com/lambdawalker/go.apexfission.aws.auth/feature"
	composer "github.com/lambdawalker/go.infra.composer"
)

func binding() RuntimeBinding {
	return RuntimeBinding{TableName: "shared-runtime", TableARN: "arn:aws:dynamodb:us-east-2:123456789012:table/shared-runtime"}
}
func TestApplicationInjectsAuthenticationAndAdditionalFeature(t *testing.T) {
	sig := &composer.Feature{ID: "signature", Requirements: []composer.Requirement{{ID: "identity", Contract: auth.IdentityContract()}}, Resources: []composer.Resource{{ID: "verify", Kind: "example:signature", Inputs: map[string]composer.Value{"issuer": composer.RequirementRef("signature", "identity", "issuer")}}}}
	app, err := New(binding(), sig)
	if err != nil {
		t.Fatal(err)
	}
	p, err := app.Plan(composer.Environment{Name: "qa"})
	if err != nil {
		t.Fatal(err)
	}
	order := p.Order()
	positions := map[string]int{}
	for i, name := range order {
		positions[name] = i
	}
	if len(order) != 6 || positions["card-capture.storage"] >= positions["authentication.backend"] || positions["authentication.backend"] >= positions["card-capture.backend"] || positions["authentication.backend"] >= positions["email-verification.backend"] || positions["authentication.backend"] >= positions["signature.verify"] {
		t.Fatal(order)
	}
	if p.Bindings()[0].Origin != "explicit" {
		t.Fatal(p.Bindings())
	}
}
func TestIncompleteRuntimeAndDuplicateNativeAuthFail(t *testing.T) {
	if _, err := New(RuntimeBinding{}); err == nil {
		t.Fatal("missing table accepted")
	}
	if _, err := New(binding(), auth.Declare("second-auth")); err == nil {
		t.Fatal("unsupported second native auth instance accepted")
	}
	b := binding()
	b.TestOnly = true
	app, err := New(b)
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Plan(composer.Environment{Name: "prod"})
	var e *composer.ValidationError
	if !errors.As(err, &e) || e.Diagnostics[0].Code != "IC_TEST_PROVIDER" {
		t.Fatal(err)
	}
}
func TestAppConfigurationUsesDeclaredSecureMechanisms(t *testing.T) {
	p, err := AuthenticationConfiguration("dev", AuthSettings{AppOrigin: "https://app.example.test", SenderAddress: "verify@example.test", CaptureEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Bindings()) != 3 {
		t.Fatal(p)
	}
	for _, b := range p.Bindings() {
		if b.Property == "proof_key" && (b.Source.Storage != "secretsmanager" || !b.Source.SecretDelivery || b.Initial != nil) {
			t.Fatal("insecure proof binding")
		}
	}
	if _, err := AuthenticationConfiguration("dev", AuthSettings{AppOrigin: "https://user:password@example.test", SenderAddress: "verify@example.test"}); err == nil {
		t.Fatal("credential-bearing public origin accepted")
	}
}

func TestEmailVerificationIsASeparateExplicitFeature(t *testing.T) {
	app, err := New(binding())
	if err != nil {
		t.Fatal(err)
	}
	p, err := app.Plan(composer.Environment{Name: "qa"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Resources()) != 5 {
		t.Fatalf("expected shared state plus three independent features and capture storage, got %d", len(p.Resources()))
	}
	found := false
	for _, b := range p.Bindings() {
		if strings.HasPrefix(b.Consumer, "email-verification.") {
			found = true
			if b.Origin != "explicit" {
				t.Fatal("implicit email integration")
			}
		}
	}
	if !found {
		t.Fatal("missing verification integration")
	}
}

func TestVerificationConfigurationOwnsSecretAndDiagnostics(t *testing.T) {
	p, err := EmailVerificationConfiguration("dev", AuthSettings{AppOrigin: "https://app.example.test", SenderAddress: "verify@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Bindings()) != 4 {
		t.Fatal(p)
	}
	for _, b := range p.Bindings() {
		if b.Property == "proof_key" && (b.Source.Storage != "secretsmanager" || !b.Source.SecretDelivery || b.Initial != nil) {
			t.Fatal("insecure verification key binding")
		}
	}
}
