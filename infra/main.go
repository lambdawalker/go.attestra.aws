// This program owns a new application's stack. It does not adopt existing Attestra state.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/lambdawalker/go.apexfission.aws.auth/auth"
	infra "github.com/lambdawalker/go.apexfission.aws.auth/pulumi"
	"github.com/lambdawalker/go.apexfission.aws.auth/router"
	"github.com/lambdawalker/go.attestra.aws/app"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/apigatewayv2"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ses"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

type Settings struct {
	AppOrigin, AdminOrigin, SenderDomain, SenderAddress, AdminCertificateARN, Archives string
	ProofKey                                                                           pulumi.StringInput
}

func (s Settings) Validate() error {
	for _, raw := range []string{s.AppOrigin, s.AdminOrigin} {
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("appOrigin/adminOrigin must be HTTPS origins")
		}
	}
	if !strings.Contains(s.AdminCertificateARN, ":acm:") || s.Archives == "" || s.ProofKey == nil || s.SenderDomain == "" || !strings.HasSuffix(s.SenderAddress, "@"+s.SenderDomain) {
		return fmt.Errorf("sender settings, adminCertificateArn, archive directory and secret proofKey are required")
	}
	return nil
}
func main() { pulumi.Run(provision) }
func provision(ctx *pulumi.Context) error {
	awsConfig := config.New(ctx, "aws")
	var accounts []string
	if json.Unmarshal([]byte(awsConfig.Get("allowedAccountIds")), &accounts) != nil || len(accounts) != 1 || !regexp.MustCompile(`^[0-9]{12}$`).MatchString(accounts[0]) || accounts[0] == "000000000000" || awsConfig.Get("region") == "" {
		return fmt.Errorf("pin aws:region and exactly one real aws:allowedAccountIds before provisioning")
	}
	cfg := config.New(ctx, "")
	if cfg.GetBool("senderOnly") {
		return bootstrapSender(ctx, cfg.Require("senderDomain"))
	}
	key := cfg.RequireSecret("proofKey").ApplyT(func(v string) (string, error) {
		b, e := base64.StdEncoding.DecodeString(v)
		if e != nil || len(b) < 32 {
			return "", fmt.Errorf("proofKey must be base64 of at least 32 random bytes")
		}
		return v, nil
	}).(pulumi.StringOutput)
	archives := cfg.Get("archiveDirectory")
	if archives == "" {
		archives = "../dist"
	}
	return compose(ctx, Settings{AppOrigin: cfg.Require("appOrigin"), AdminOrigin: cfg.Require("adminOrigin"), SenderDomain: cfg.Require("senderDomain"), SenderAddress: cfg.Require("senderAddress"), AdminCertificateARN: cfg.Require("adminCertificateArn"), Archives: archives, ProofKey: key})
}
func compose(ctx *pulumi.Context, s Settings) error {
	if e := s.Validate(); e != nil {
		return e
	}
	verification, e := ses.NewDomainIdentityVerification(ctx, "sender-verified", &ses.DomainIdentityVerificationArgs{Domain: pulumi.String(s.SenderDomain)})
	if e != nil {
		return e
	}
	a, e := infra.NewAuth(ctx, "auth", infra.AuthArgs{Origin: s.AppOrigin, SenderDomain: s.SenderDomain, SenderAddress: s.SenderAddress, ProofKey: s.ProofKey, ChallengeArchive: filepath.Join(s.Archives, "challenge.zip")}, pulumi.Transformations([]pulumi.ResourceTransformation{func(args *pulumi.ResourceTransformationArgs) *pulumi.ResourceTransformationResult {
		if args.Type != "aws:cognito/userPool:UserPool" {
			return nil
		}
		return &pulumi.ResourceTransformationResult{Props: args.Props, Opts: append(args.Opts, pulumi.DependsOn([]pulumi.Resource{verification}))}
	}}))
	if e != nil {
		return e
	}
	environment := pulumi.StringMap{}
	for k, v := range a.Environment {
		environment[k] = v
	}
	environment["MODE"] = pulumi.String("auth")
	environment["ENVIRONMENT"] = pulumi.String(ctx.Stack())
	fn, e := infra.NewFunction(ctx, "application", infra.FunctionArgs{Archive: filepath.Join(s.Archives, "application.zip"), Environment: environment, Policy: a.Policy})
	if e != nil {
		return e
	}
	application, e := app.Compose(auth.Module{})
	if e != nil {
		return e
	}
	routes := []router.Route{}
	for _, r := range application.Routes {
		if !r.Admin {
			routes = append(routes, r)
		}
	}
	api, e := infra.NewAPI(ctx, "application", infra.APIArgs{Routes: routes, Functions: map[string]*lambda.Function{"application": fn}, RuntimeEnforced: map[string]bool{"application": true}, Issuer: a.Issuer, ClientID: a.Client.ID(), Origin: s.AppOrigin})
	if e != nil {
		return e
	}
	administration, e := infra.NewAdminWithLogin(ctx, "admin", s.AdminOrigin)
	if e != nil {
		return e
	}
	adminFn, e := infra.NewFunction(ctx, "admin", infra.FunctionArgs{Archive: filepath.Join(s.Archives, "application.zip"), Environment: pulumi.StringMap{"MODE": pulumi.String("admin"), "ENVIRONMENT": pulumi.String(ctx.Stack()), "ISSUER": administration.Issuer, "CLIENT_ID": administration.Client.ID(), "CONFIG_TABLE": administration.Table.Name, "LOGIN_DOMAIN": administration.LoginDomain, "ADMIN_ORIGIN": pulumi.String(s.AdminOrigin)}, Policy: pulumi.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["dynamodb:GetItem","dynamodb:Query","dynamodb:PutItem","dynamodb:ConditionCheckItem"],"Resource":%q,"Condition":{"ForAllValues:StringLike":{"dynamodb:LeadingKeys":["ENV#%s#MODULE#*"]}}},{"Effect":"Allow","Action":["dynamodb:GetItem","dynamodb:ConditionCheckItem"],"Resource":%q,"Condition":{"ForAllValues:StringEquals":{"dynamodb:LeadingKeys":["GRANT#%s"]}}}]}`, administration.Table.Arn, ctx.Stack(), administration.Table.Arn, ctx.Stack())})
	if e != nil {
		return e
	}
	adminAPI, e := infra.NewAdminAPI(ctx, "admin", adminFn, true)
	if e != nil {
		return e
	}
	origin, _ := url.Parse(s.AdminOrigin)
	domain, e := apigatewayv2.NewDomainName(ctx, "admin-domain", &apigatewayv2.DomainNameArgs{DomainName: pulumi.String(origin.Hostname()), DomainNameConfiguration: &apigatewayv2.DomainNameDomainNameConfigurationArgs{CertificateArn: pulumi.String(s.AdminCertificateARN), EndpointType: pulumi.String("REGIONAL"), SecurityPolicy: pulumi.String("TLS_1_2")}})
	if e != nil {
		return e
	}
	_, e = apigatewayv2.NewApiMapping(ctx, "admin-domain-mapping", &apigatewayv2.ApiMappingArgs{ApiId: adminAPI.API.ID(), DomainName: domain.DomainName, Stage: pulumi.String("$default")}, pulumi.DependsOn([]pulumi.Resource{adminAPI}))
	if e != nil {
		return e
	}
	ctx.Export("apiUrl", api.URL)
	ctx.Export("healthUrl", pulumi.Sprintf("%s/health", api.URL))
	ctx.Export("adminUrl", pulumi.String(s.AdminOrigin+"/admin"))
	ctx.Export("adminRawApiUrl", adminAPI.URL)
	ctx.Export("adminDnsTarget", domain.DomainNameConfiguration.TargetDomainName())
	ctx.Export("userPoolId", a.Pool.ID())
	ctx.Export("clientId", a.Client.ID())
	ctx.Export("issuer", a.Issuer)
	ctx.Export("adminPoolId", administration.Pool.ID())
	ctx.Export("adminClientId", administration.Client.ID())
	ctx.Export("configTableName", administration.Table.Name)
	ctx.Export("sesVerificationToken", a.Sender.VerificationToken)
	ctx.Export("sesDkimTokens", a.DKIM.DkimTokens)
	return nil
}
