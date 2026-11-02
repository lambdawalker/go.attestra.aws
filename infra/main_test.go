package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lambdawalker/go.apexfission.aws.auth/artifacts"
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
	var inventory []any
	if err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		if err := deploy(ctx); err != nil {
			return err
		}
		pulumi.All(ctx.GetCurrentExportMap()["resourceInventory"]).ApplyT(func(v []any) error { inventory = v[0].([]any); return nil })
		return nil
	}, pulumi.WithMocks("attestra-application", "dev", m)); err != nil {
		t.Fatal(err)
	}
	nativeCount := 0
	for _, r := range m.resources {
		if strings.HasPrefix(r.TypeToken, "aws:") {
			nativeCount++
		}
	}
	if len(inventory) != nativeCount {
		t.Fatalf("main inventory coverage %d of %d native resources", len(inventory), nativeCount)
	}
	serialized, _ := json.Marshal(inventory)
	if strings.Contains(string(serialized), cfg["attestra-application:proofKey"]) {
		t.Fatal("inventory leaks proof key")
	}
	for _, r := range m.resources {
		if r.Name == "email-signup" {
			if got := r.Inputs["description"].StringValue(); got != "app.example.test DEV | go.apexfission.aws.auth" {
				t.Fatal(got)
			}
			tags := r.Inputs["tags"]
			if !tags.IsObject() || tags.ObjectValue()["ic:application"].StringValue() != "attestra" || tags.ObjectValue()["ic:feature"].StringValue() != "authentication" {
				t.Fatal("main app is missing ownership on its integrated email Lambda")
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
	if count != 20 {
		t.Fatalf("expected20featureLambdas,got%d", count)
	}
}
