package main

import (
	"fmt"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ses"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"strings"
)

// bootstrapSender provisions the same component/child URNs used by the pinned
// framework so DNS can be verified before its Cognito pool is created. Changing
// the dependency version requires rechecking these names against NewAuth.
func bootstrapSender(ctx *pulumi.Context, domain string) error {
	if domain == "" || strings.ContainsAny(domain, " /:@") {
		return fmt.Errorf("senderDomain must be a DNS domain")
	}
	parent := &pulumi.ResourceState{}
	if e := ctx.RegisterComponentResource("apexfission:auth:Core", "auth", parent); e != nil {
		return e
	}
	identity, e := ses.NewDomainIdentity(ctx, "auth-sender", &ses.DomainIdentityArgs{Domain: pulumi.String(domain)}, pulumi.Parent(parent))
	if e != nil {
		return e
	}
	dkim, e := ses.NewDomainDkim(ctx, "auth-dkim", &ses.DomainDkimArgs{Domain: identity.Domain}, pulumi.Parent(parent))
	if e != nil {
		return e
	}
	ctx.Export("sesVerificationToken", identity.VerificationToken)
	ctx.Export("sesDkimTokens", dkim.DkimTokens)
	return ctx.RegisterResourceOutputs(parent, pulumi.Map{"sesVerification": identity.VerificationToken, "dkimTokens": dkim.DkimTokens})
}
