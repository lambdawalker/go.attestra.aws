package main

import (
	"encoding/base64"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"strings"
	"sync"
	"testing"
)

type mocks struct {
	mu        sync.Mutex
	resources []pulumi.MockResourceArgs
}

func (m *mocks) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return resource.PropertyMap{}, nil
}
func (m *mocks) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resources = append(m.resources, a)
	out := a.Inputs.Copy()
	out["arn"] = resource.NewStringProperty("arn:aws:service:us-east-1:123456789012:" + a.Name)
	out["endpoint"] = resource.NewStringProperty("cognito-idp.us-east-1.amazonaws.com/pool")
	out["invokeArn"] = out["arn"]
	out["executionArn"] = out["arn"]
	out["apiEndpoint"] = resource.NewStringProperty("https://api.example")
	return a.Name + "-id", out, nil
}
func TestApplicationInfrastructure(t *testing.T) {
	m := &mocks{}
	e := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return compose(ctx, Settings{AppOrigin: "https://app.example", AdminOrigin: "https://admin.example", SenderDomain: "mail.example", SenderAddress: "hello@mail.example", AdminCertificateARN: "arn:aws:acm:us-east-1:123456789012:certificate/test", Archives: t.TempDir(), ProofKey: pulumi.String(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))))})
	}, pulumi.WithMocks("attestra", "dev", m))
	if e != nil {
		t.Fatal(e)
	}
	counts := map[string]int{}
	for _, r := range m.resources {
		counts[r.TypeToken]++
		if r.TypeToken == "aws:cognito/userPool:UserPool" && r.Name == "auth-users" {
			found := false
			for _, d := range r.RegisterRPC.GetDependencies() {
				if strings.Contains(d, "sender-verified") {
					found = true
				}
			}
			if !found {
				t.Error("auth pool does not wait for SES verification")
			}
		}
		if r.TypeToken == "aws:cognito/userPool:UserPool" || r.TypeToken == "aws:dynamodb/table:Table" {
			if !r.RegisterRPC.GetProtect() {
				t.Errorf("unprotected %s", r.Name)
			}
		}
		if r.TypeToken == "aws:lambda/function:Function" && r.Name == "admin" {
			vars := r.Inputs["environment"].ObjectValue()["variables"].ObjectValue()
			if _, exists := vars["PROOF_KEY"]; exists {
				t.Fatal("admin received customer proof key")
			}
		}
		if r.TypeToken == "aws:apigatewayv2/route:Route" {
			key := r.Inputs["routeKey"].StringValue()
			if key == "GET /v1/me" && r.Inputs["authorizationType"].StringValue() != "JWT" {
				t.Fatal("profile route public")
			}
		}
	}
	if counts["aws:lambda/function:Function"] != 3 || counts["aws:cognito/userPool:UserPool"] != 2 || counts["aws:dynamodb/table:Table"] != 2 || counts["aws:apigatewayv2/apiMapping:ApiMapping"] != 1 {
		t.Fatal(counts)
	}
}
func TestMissingAdminCertificateRejected(t *testing.T) {
	if e := (Settings{}).Validate(); e == nil {
		t.Fatal("incomplete configuration accepted")
	}
}
