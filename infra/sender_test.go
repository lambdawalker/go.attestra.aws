package main

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"testing"
)

func TestSenderBootstrapCreatesOnlyStableSenderResources(t *testing.T) {
	m := &mocks{}
	e := pulumi.RunErr(func(ctx *pulumi.Context) error { return bootstrapSender(ctx, "mail.example") }, pulumi.WithMocks("attestra", "dev", m))
	if e != nil {
		t.Fatal(e)
	}
	found := map[string]string{}
	for _, r := range m.resources {
		if r.TypeToken == "aws:ses/domainIdentity:DomainIdentity" || r.TypeToken == "aws:ses/domainDkim:DomainDkim" {
			found[r.Name] = r.RegisterRPC.GetParent()
		} else if r.TypeToken != "apexfission:auth:Core" {
			t.Fatal("unexpected bootstrap resource", r.TypeToken)
		}
	}
	parent := "urn:pulumi:dev::attestra::apexfission:auth:Core::auth"
	if found["auth-sender"] != parent || found["auth-dkim"] != parent {
		t.Fatal("sender identity differs from pinned framework", found)
	}
}
