package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lambdawalker/go.apexfission.aws.auth/envreset"
	"github.com/lambdawalker/go.attestra.aws/artifacts"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type monitor struct {
	mu        sync.Mutex
	resources []pulumi.MockResourceArgs
}

func (m *monitor) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return resource.NewPropertyMapFromMap(map[string]any{"name": "us-east-2", "accountId": "123456789012"}), nil
}
func (m *monitor) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resources = append(m.resources, a)
	out := a.Inputs.Copy()
	out["arn"] = resource.NewStringProperty("arn:aws:test:us-east-2:123456789012:" + a.Name)
	out["name"] = resource.NewStringProperty(a.Name)
	out["url"] = resource.NewStringProperty("https://sqs.example.test/" + a.Name)
	out["invokeArn"] = resource.NewStringProperty("arn:invoke:" + a.Name)
	out["apiEndpoint"] = resource.NewStringProperty("https://api.example.test")
	out["executionArn"] = resource.NewStringProperty("arn:aws:execute-api:us-east-2:123456789012:api")
	out["dkimTokens"] = resource.NewArrayProperty([]resource.PropertyValue{resource.NewStringProperty("one"), resource.NewStringProperty("two"), resource.NewStringProperty("three")})
	out["versionId"] = resource.NewStringProperty(strings.Repeat("v", 32))
	return a.Name + "-id", out, nil
}
func TestAppPreflightRejectsMissingRuntimeWithoutAWS(t *testing.T) {
	t.Setenv("PULUMI_CONFIG", "{}")
	m := &monitor{}
	err := pulumi.RunErr(deploy, pulumi.WithMocks("attestra-application", "dev", m))
	if err == nil || !strings.Contains(err.Error(), "shared runtime") {
		t.Fatal(err)
	}
	for _, r := range m.resources {
		if strings.HasPrefix(r.TypeToken, "aws:") {
			t.Fatal("preflightmutated", r.Name)
		}
	}
}
func TestApplicationComposesPinnedFeatureAndPreservesSecretBoundary(t *testing.T) {
	dir := t.TempDir()
	for _, a := range artifacts.Lambdas() {
		if err := os.WriteFile(filepath.Join(dir, a.Name+".zip"), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := map[string]string{"attestra-application:artifactDir": dir, "attestra-application:runtimeTableName": "shared-runtime", "attestra-application:runtimeTableArn": "arn:aws:dynamodb:us-east-2:123456789012:table/shared-runtime", "attestra-application:appOrigin": "https://App.Example.Test", "attestra-application:senderDomain": "mail.example.test", "attestra-application:senderAddress": "verify@mail.example.test", "attestra-application:proofKey": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))}
	data, _ := json.Marshal(cfg)
	t.Setenv("PULUMI_CONFIG", string(data))
	m := &monitor{}
	var inventoryRows []any
	if err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		if err := deploy(ctx); err != nil {
			return err
		}
		pulumi.All(ctx.GetCurrentExportMap()["resourceInventory"]).ApplyT(func(v []any) error { inventoryRows = v[0].([]any); return nil })
		return nil
	}, pulumi.WithMocks("attestra-application", "dev", m)); err != nil {
		t.Fatal(err)
	}
	awsCount := 0
	for _, r := range m.resources {
		if strings.HasPrefix(r.TypeToken, "aws:") {
			awsCount++
		}
	}
	if len(inventoryRows) != awsCount {
		t.Fatalf("inventory omitted native resources: got %d, want %d", len(inventoryRows), awsCount)
	}
	serialized, _ := json.Marshal(inventoryRows)
	if strings.Contains(string(serialized), cfg["attestra-application:proofKey"]) {
		t.Fatal("inventory exposed proof key")
	}
	for _, row := range inventoryRows {
		r := row.(map[string]any)
		if r["type"] == "aws:apigatewayv2/route:Route" && r["parentId"] == "" {
			t.Fatal("route lost API parent")
		}
	}
	for name, want := range map[string]string{"email-signup": "email-verification", "email-http-api": "authentication", "environment-reset": "application", "capture": "card-capture", "capture-worker": "card-capture", "capture-dispatcher": "card-capture", "id-captures": "card-capture", "id-evidence": "card-capture"} {
		var item pulumi.MockResourceArgs
		for _, r := range m.resources {
			if r.Name == name {
				item = r
			}
		}
		owner := "go.attestra.aws"
		if want == "authentication" {
			owner = "go.apexfission.aws.auth"
		}
		if want == "card-capture" {
			owner = "go.appexfission.aws.card-capture"
		}
		if want == "email-verification" {
			owner = "go.apexfission.aws.verifyemail"
		}
		if description := item.Inputs["description"]; description.IsString() && description.StringValue() != "app.example.test DEV | "+owner {
			t.Fatal("incorrect creating repository", name, description)
		}
		if item.Inputs["tags"].ObjectValue()["ic:feature"].StringValue() != want {
			t.Fatal("feature attribution lost", name)
		}
	}
	for _, r := range m.resources {
		switch r.TypeToken {
		case "aws:lambda/function:Function", "aws:dynamodb/table:Table", "aws:iam/role:Role", "aws:apigatewayv2/api:Api", "aws:apigatewayv2/stage:Stage", "aws:sqs/queue:Queue", "aws:s3/bucketV2:BucketV2", "aws:secretsmanager/secret:Secret", "aws:cognito/userPool:UserPool":
			tags := r.Inputs["tags"]
			if !tags.IsObject() || tags.ObjectValue()["ic:application"].StringValue() != "attestra" {
				t.Fatalf("missing ownership tags on %s (%s)", r.Name, r.TypeToken)
			}
		}
	}

	count := 0
	for _, r := range m.resources {
		if r.TypeToken == "aws:lambda/function:Function" {
			count++
			if r.Name == "email-signup" {
				env := r.Inputs["environment"].ObjectValue()["variables"].ObjectValue()
				if _, ok := env["PROOF_KEY"]; ok {
					t.Fatal("raw proof key in environment")
				}
				if env["PROOF_KEY_SECRET_ARN"].StringValue() == "" {
					t.Fatal("missingsecretref")
				}
			}
		}
	}
	byName := map[string]pulumi.MockResourceArgs{}
	for _, r := range m.resources {
		byName[r.Name] = r
	}
	reset := byName["environment-reset"].Inputs["environment"].ObjectValue()["variables"].ObjectValue()["RESET_MANIFEST"].StringValue()
	var manifest struct{ Functions, Tables []string }
	if err := json.Unmarshal([]byte(reset), &manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := envreset.ParseManifest([]byte(reset), "environment-reset-control"); err != nil {
		t.Fatalf("composed reset worker cannot start: %v", err)
	}
	if len(manifest.Functions) != 18 {
		t.Fatalf("reset omits feature writers: %v", manifest.Functions)
	}
	has := func(items []string, s string) bool {
		for _, v := range items {
			if v == s {
				return true
			}
		}
		return false
	}
	for _, n := range []string{"signup", "resend", "confirm"} {
		if !has(manifest.Functions, "email-"+n) {
			t.Fatal("verification writer missing from reset", n)
		}
		route := byName["verifyemail-"+n+"-route"].Inputs
		if route["routeKey"].StringValue() != "POST /"+n || route["apiId"].StringValue() != "email-http-api-id" {
			t.Fatal("route compatibility lost", route)
		}
		env := byName["email-"+n].Inputs["environment"].ObjectValue()["variables"].ObjectValue()
		if env["GRANT_TABLE"].StringValue() != "email-proofs" || env["TABLE_NAME"].StringValue() != "verification-proofs" {
			t.Fatal("grant/proof bindings mixed")
		}
	}
	if !has(manifest.Tables, "verification-proofs") || !has(manifest.Tables, "email-proofs") {
		t.Fatal("reset omits proof/grant tables", manifest.Tables)
	}
	policy := byName["environment-reset-data"].Inputs["policy"].StringValue()
	for _, resource := range []string{"verification-proofs", "email-signup", "email-resend", "email-confirm"} {
		if !strings.Contains(policy, resource) {
			t.Fatal("reset permission missing", resource)
		}
	}

	if count != 20 {
		t.Fatalf("expected20featureLambdas,got%d", count)
	}
}

func TestMissingFeatureArchiveFailsWholeAppBeforeRegistration(t *testing.T) {
	for _, missing := range []string{"confirm", "capture-worker"} {
		t.Run(missing, func(t *testing.T) {
			dir := t.TempDir()
			for _, a := range artifacts.Lambdas() {
				if a.Name == missing {
					continue
				}
				if err := os.WriteFile(filepath.Join(dir, a.Name+".zip"), []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cfg := map[string]string{"attestra-application:artifactDir": dir, "attestra-application:runtimeTableName": "runtime", "attestra-application:runtimeTableArn": "arn:runtime", "attestra-application:appOrigin": "https://App.Example.Test", "attestra-application:senderDomain": "example.test", "attestra-application:senderAddress": "verify@example.test", "attestra-application:proofKey": base64.StdEncoding.EncodeToString(make([]byte, 32))}
			data, _ := json.Marshal(cfg)
			t.Setenv("PULUMI_CONFIG", string(data))
			m := &monitor{}
			err := pulumi.RunErr(deploy, pulumi.WithMocks("attestra-application", "dev", m))
			if err == nil || !strings.Contains(err.Error(), missing+".zip") {
				t.Fatal("missing feature artifact accepted", err)
			}
			for _, r := range m.resources {
				if strings.HasPrefix(r.TypeToken, "aws:") {
					t.Fatal("partial deployment before feature preflight", r.Name)
				}
			}
		})
	}
}
