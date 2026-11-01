package application

import (
	"errors"
	"reflect"
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
	if !reflect.DeepEqual(p.Order(), []string{"application-runtime.existing", "authentication.backend", "signature.verify"}) {
		t.Fatal(p.Order())
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
	if len(p.Bindings()) != 5 {
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
