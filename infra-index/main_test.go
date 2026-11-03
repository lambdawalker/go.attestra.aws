package main

import (
	"archive/zip"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type indexMocks struct {
	mu        sync.Mutex
	resources map[string]pulumi.MockResourceArgs
}

func (m *indexMocks) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return args.Args, nil
}
func (m *indexMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resources[args.Name] = args
	out := args.Inputs.Copy()
	out["arn"] = resource.NewStringProperty("arn:example:" + args.Name)
	switch args.TypeToken {
	case "aws:acm/certificate:Certificate":
		out["domainValidationOptions"] = resource.NewArrayProperty([]resource.PropertyValue{resource.NewObjectProperty(resource.NewPropertyMapFromMap(map[string]any{"resourceRecordName": "_token.index.example.com", "resourceRecordType": "CNAME", "resourceRecordValue": "_token.acm-validations.aws"}))})
	case "aws:apigatewayv2/api:Api":
		out["apiEndpoint"] = resource.NewStringProperty("https://abc.execute-api.us-east-2.amazonaws.com")
		out["executionArn"] = resource.NewStringProperty("arn:aws:execute-api:us-east-2:123456789012:abc")
	case "aws:apigatewayv2/domainName:DomainName":
		cfg := out["domainNameConfiguration"].ObjectValue().Copy()
		cfg["targetDomainName"] = resource.NewStringProperty("target.execute-api.us-east-2.amazonaws.com")
		out["domainNameConfiguration"] = resource.NewObjectProperty(cfg)
	}
	return args.Name + "-id", out, nil
}
func TestIndexInfrastructure(t *testing.T) {
	for _, zone := range []string{"", "Z123EXAMPLE"} {
		t.Run("zone="+zone, func(t *testing.T) {
			root := t.TempDir()
			os.MkdirAll(filepath.Join(root, "infra-index"), 0755)
			os.MkdirAll(filepath.Join(root, "dist"), 0755)
			f, _ := os.Create(filepath.Join(root, "dist/index.zip"))
			z := zip.NewWriter(f)
			entry, _ := z.Create("bootstrap")
			entry.Write([]byte("test"))
			z.Close()
			f.Close()
			data, _ := os.ReadFile(filepath.Join(root, "dist/index.zip"))
			os.WriteFile(filepath.Join(root, "dist/admin.zip"), data, 0600)
			t.Chdir(filepath.Join(root, "infra-index"))
			t.Setenv("PULUMI_CONFIG", `{"attestra-control-plane:adminDomain":"admin.example.com","attestra-control-plane:domain":"index.example.com","attestra-control-plane:sourceHash":"test","attestra-control-plane:route53ZoneId":"`+zone+`"}`)
			m := &indexMocks{resources: map[string]pulumi.MockResourceArgs{}}
			if e := pulumi.RunErr(deployIndex, pulumi.WithMocks("attestra-control-plane", "shared", m)); e != nil {
				t.Fatal(e)
			}
			if got := m.resources["index-handler"].Inputs["description"].StringValue(); got != "index.example.com SHARED | go.attestra.aws" {
				t.Fatal(got)
			}
			for _, name := range []string{"admin-handler", "index-handler", "admin-runtime", "environment-registry", "attestra-admins"} {
				tags := m.resources[name].Inputs["tags"]
				if !tags.IsObject() || tags.ObjectValue()["ic:environment"].StringValue() != "shared" {
					t.Fatalf("shared control-plane ownership missing on %s", name)
				}
			}

			if m.resources["index-write"].Inputs["authorizationType"].StringValue() != "AWS_IAM" {
				t.Fatal("unauthenticated registry writes")
			}
			if m.resources["index-read"].Inputs["authorizationType"].StringValue() != "NONE" {
				t.Fatal("discovery is not public")
			}
			if m.resources["index-handler"].Inputs["reservedConcurrentExecutions"].NumberValue() != -1 {
				t.Fatal("registry reserves scarce Lambda capacity")
			}
			_, dns := m.resources["index-hostname"]
			if dns != (zone != "") {
				t.Fatal("wrong DNS provider path")
			}
			if !m.resources["admin-runtime"].RegisterRPC.GetProtect() || !m.resources["admin-runtime"].Inputs["deletionProtectionEnabled"].BoolValue() {
				t.Fatal("runtime data lacks deletion protection")
			}
			pool := m.resources["attestra-admins"].Inputs
			if pool["mfaConfiguration"].StringValue() != "ON" || !pool["adminCreateUserConfig"].ObjectValue()["allowAdminCreateUserOnly"].BoolValue() {
				t.Fatal("admin identity boundary weakened")
			}
			adminEnv := m.resources["admin-handler"].Inputs["environment"].ObjectValue()["variables"].ObjectValue()
			if got := adminEnv["RESET_FUNCTION_PREFIX"]; !got.IsString() || got.StringValue() != "attestra-app-auth-reset-" {
				t.Fatal("admin reset invocation prefix differs from application deployment")
			}
			policy := m.resources["admin-reset-invoke"].Inputs["policy"].StringValue()
			if !strings.Contains(policy, ":function:attestra-app-auth-reset-*") || strings.Contains(policy, ":function:attestra-reset-*") {
				t.Fatal("reset invocation IAM targets wrong application")
			}
			if m.resources["admin-api"].Inputs["corsConfiguration"].HasValue() {
				t.Fatal("admin API should remain same origin")
			}
			if m.resources["admin-publish"].Inputs["authorizerId"].StringValue() != "admin-jwt-id" {
				t.Fatal("admin publish lacks dedicated authorizer")
			}
			for _, name := range []string{"admin-artifacts", "admin-artifacts-report"} {
				route := m.resources[name].Inputs
				if route["authorizationType"].StringValue() != "JWT" || route["authorizerId"].StringValue() != "admin-jwt-id" || route["authorizationScopes"].ArrayValue()[0].StringValue() != "aws.cognito.signin.user.admin" {
					t.Fatal("artifact route lacks native scoped JWT", name)
				}
			}
			if !m.resources["environment-registry"].RegisterRPC.GetProtect() {
				t.Fatal("shared registry lacks deletion protection")
			}
		})
	}
}

// Keep the independently built Pulumi module aligned with the application's
// reset target API without adding a runtime dependency to this native program.
func TestResetPrefixMatchesApplicationAPI(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../adminapi/reset.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range spec.Names {
			if name.Name != "ResetFunctionPrefix" {
				continue
			}
			literal, ok := spec.Values[i].(*ast.BasicLit)
			if !ok {
				t.Fatal("reset prefix must be a literal application contract")
			}
			got, err := strconv.Unquote(literal.Value)
			if err != nil || got != resetFunctionPrefix {
				t.Fatalf("native reset prefix %q differs from adminapi %q", resetFunctionPrefix, got)
			}
			found = true
		}
		return true
	})
	if !found {
		t.Fatal("missing application reset prefix contract")
	}
}

func TestSharedConfigurationFailsBeforeAWSRegistration(t *testing.T) {
	for _, tt := range []struct{ name, key, value, message string }{
		{"missing domain", "domain", "", "domain is required"},
		{"missing adminDomain", "adminDomain", "", "adminDomain is required"},
		{"missing sourceHash", "sourceHash", "", "sourceHash is required"},
		{"blank sourceHash", "sourceHash", "  ", "sourceHash is required"},
		{"URL", "domain", "https://private-invalid.example/path", "domain must be a DNS hostname"},
		{"wildcard", "adminDomain", "*.private-invalid.example", "adminDomain must be a DNS hostname"},
		{"invalid label", "domain", "bad_.private-invalid.example", "domain must be a DNS hostname"},
		{"IP address", "domain", "192.0.2.1", "domain must be a DNS hostname"},
		{"same host", "domain", "ADMIN.EXAMPLE.COM", "domain and adminDomain must be distinct"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := map[string]string{"attestra-control-plane:domain": "index.example.com", "attestra-control-plane:adminDomain": "admin.example.com", "attestra-control-plane:sourceHash": "test"}
			if tt.value == "" {
				delete(cfg, "attestra-control-plane:"+tt.key)
			} else {
				cfg["attestra-control-plane:"+tt.key] = tt.value
			}
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("PULUMI_CONFIG", string(data))
			m := &indexMocks{resources: map[string]pulumi.MockResourceArgs{}}
			err = pulumi.RunErr(deployIndex, pulumi.WithMocks("attestra-control-plane", "shared", m))
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("expected static configuration error %q, got %v", tt.message, err)
			}
			if strings.Contains(err.Error(), "private-invalid") {
				t.Fatal("configuration error reveals supplied values")
			}
			if len(m.resources) != 0 {
				t.Fatalf("invalid configuration registered %d AWS resources", len(m.resources))
			}
		})
	}
}
