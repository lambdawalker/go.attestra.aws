package main

import "github.com/lambdawalker/go.infra.composer/adapters/pulumi/awslabels"

import (
	"fmt"
	"github.com/lambdawalker/go.infra.composer/ownership"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/acm"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/apigatewayv2"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/dynamodb"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/route53"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
	"net"
	"path/filepath"
	"strings"
)

func main() { pulumi.Run(deployIndex) }
func deployIndex(ctx *pulumi.Context) error {
	cfg := config.New(ctx, "")
	if err := validateSharedConfiguration(cfg); err != nil {
		return err
	}
	domainLabel := cfg.Get("ownershipDomain")
	if domainLabel == "" {
		domainLabel = cfg.Require("domain")
	}
	ctx, labels, labelErr := awslabels.Install(ctx, ownership.Metadata{Application: "attestra", Environment: "shared", Domain: domainLabel, Feature: "control-plane", Deployment: "attestra-control-plane"})
	if labelErr != nil {
		return labelErr
	}
	runtimeTable, e := deployAdmin(ctx, cfg)
	if e != nil {
		return e
	}
	domain := cfg.Require("domain")
	zone := cfg.Get("route53ZoneId")
	cert, e := acm.NewCertificate(ctx, "index-certificate", &acm.CertificateArgs{DomainName: pulumi.String(domain), ValidationMethod: pulumi.String("DNS")}, awslabels.Option(ctx))
	if e != nil {
		return e
	}
	ctx.Export("certificateArn", cert.Arn)
	var validationResources []pulumi.Resource
	if zone != "" {
		option := cert.DomainValidationOptions.Index(pulumi.Int(0))
		record, e := route53.NewRecord(ctx, "index-certificate-dns", &route53.RecordArgs{ZoneId: pulumi.String(zone), Name: option.ResourceRecordName().Elem(), Type: option.ResourceRecordType().Elem(), Records: pulumi.StringArray{option.ResourceRecordValue().Elem()}, Ttl: pulumi.Int(300)}, awslabels.Option(ctx))
		if e != nil {
			return e
		}
		validationResources = append(validationResources, record)
	}
	validated, e := acm.NewCertificateValidation(ctx, "index-certificate-validation", &acm.CertificateValidationArgs{CertificateArn: cert.Arn}, pulumi.DependsOn(validationResources), awslabels.Option(ctx))
	if e != nil {
		return e
	}
	table, e := dynamodb.NewTable(ctx, "environment-registry", &dynamodb.TableArgs{BillingMode: pulumi.String("PAY_PER_REQUEST"), HashKey: pulumi.String("id"), Attributes: dynamodb.TableAttributeArray{&dynamodb.TableAttributeArgs{Name: pulumi.String("id"), Type: pulumi.String("S")}}, PointInTimeRecovery: &dynamodb.TablePointInTimeRecoveryArgs{Enabled: pulumi.Bool(true)}, ServerSideEncryption: &dynamodb.TableServerSideEncryptionArgs{Enabled: pulumi.Bool(true)}}, pulumi.Protect(true), awslabels.Option(ctx))
	if e != nil {
		return e
	}
	role, e := iam.NewRole(ctx, "index-role", &iam.RoleArgs{AssumeRolePolicy: pulumi.String(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"},"Action":"sts:AssumeRole"}]}`)}, awslabels.Option(ctx))
	if e != nil {
		return e
	}
	logs, e := iam.NewRolePolicyAttachment(ctx, "index-logs", &iam.RolePolicyAttachmentArgs{Role: role.Name, PolicyArn: pulumi.String("arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole")}, awslabels.Option(ctx))
	if e != nil {
		return e
	}
	grant, e := iam.NewRolePolicy(ctx, "index-data", &iam.RolePolicyArgs{Role: role.Name, Policy: pulumi.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["dynamodb:GetItem","dynamodb:UpdateItem","dynamodb:Scan","dynamodb:ConditionCheckItem"],"Resource":%q},{"Effect":"Allow","Action":["dynamodb:GetItem","dynamodb:PutItem"],"Resource":%q}]}`, table.Arn, runtimeTable.Arn)}, awslabels.Option(ctx))
	if e != nil {
		return e
	}
	archive, _ := filepath.Abs("../dist/index.zip")
	fn, e := lambda.NewFunction(ctx, "index-handler", &lambda.FunctionArgs{Role: role.Arn, Runtime: pulumi.String("provided.al2023"), Handler: pulumi.String("bootstrap"), Architectures: pulumi.StringArray{pulumi.String("arm64")}, Code: pulumi.NewFileArchive(archive), MemorySize: pulumi.Int(256), Timeout: pulumi.Int(20), ReservedConcurrentExecutions: pulumi.Int(-1), Environment: &lambda.FunctionEnvironmentArgs{Variables: pulumi.StringMap{"REGISTRY_TABLE": table.Name, "RUNTIME_TABLE": runtimeTable.Name}}}, pulumi.DependsOn([]pulumi.Resource{logs, grant}), awslabels.Option(ctx))
	if e != nil {
		return e
	}
	api, e := apigatewayv2.NewApi(ctx, "index-api", &apigatewayv2.ApiArgs{ProtocolType: pulumi.String("HTTP"), CorsConfiguration: &apigatewayv2.ApiCorsConfigurationArgs{AllowOrigins: pulumi.StringArray{pulumi.String("*")}, AllowMethods: pulumi.StringArray{pulumi.String("GET")}, MaxAge: pulumi.Int(300)}}, awslabels.Option(ctx))
	if e != nil {
		return e
	}
	integration, e := apigatewayv2.NewIntegration(ctx, "index-integration", &apigatewayv2.IntegrationArgs{ApiId: api.ID(), IntegrationType: pulumi.String("AWS_PROXY"), IntegrationUri: fn.InvokeArn, PayloadFormatVersion: pulumi.String("2.0")}, awslabels.Option(ctx))
	if e != nil {
		return e
	}
	for _, r := range []struct{ name, key, auth string }{{"read", "GET /v1/environments", "NONE"}, {"write", "POST /v1/environments/{environment}/changes", "AWS_IAM"}} {
		_, e = apigatewayv2.NewRoute(ctx, "index-"+r.name, &apigatewayv2.RouteArgs{ApiId: api.ID(), RouteKey: pulumi.String(r.key), AuthorizationType: pulumi.String(r.auth), Target: pulumi.Sprintf("integrations/%s", integration.ID())}, awslabels.Option(ctx))
		if e != nil {
			return e
		}
	}
	_, e = lambda.NewPermission(ctx, "index-invoke", &lambda.PermissionArgs{Action: pulumi.String("lambda:InvokeFunction"), Function: fn.Name, Principal: pulumi.String("apigateway.amazonaws.com"), SourceArn: pulumi.Sprintf("%s/*/*", api.ExecutionArn)}, awslabels.Option(ctx))
	if e != nil {
		return e
	}
	stage, e := apigatewayv2.NewStage(ctx, "index-stage", &apigatewayv2.StageArgs{ApiId: api.ID(), Name: pulumi.String("$default"), AutoDeploy: pulumi.Bool(true), DefaultRouteSettings: &apigatewayv2.StageDefaultRouteSettingsArgs{ThrottlingBurstLimit: pulumi.Int(20), ThrottlingRateLimit: pulumi.Float64(10)}}, awslabels.Option(ctx))
	if e != nil {
		return e
	}
	custom, e := apigatewayv2.NewDomainName(ctx, "index-domain", &apigatewayv2.DomainNameArgs{DomainName: pulumi.String(domain), DomainNameConfiguration: &apigatewayv2.DomainNameDomainNameConfigurationArgs{CertificateArn: validated.CertificateArn, EndpointType: pulumi.String("REGIONAL"), SecurityPolicy: pulumi.String("TLS_1_2")}}, awslabels.Option(ctx))
	if e != nil {
		return e
	}
	_, e = apigatewayv2.NewApiMapping(ctx, "index-mapping", &apigatewayv2.ApiMappingArgs{ApiId: api.ID(), DomainName: custom.ID(), Stage: stage.Name}, awslabels.Option(ctx))
	if e != nil {
		return e
	}
	target := custom.DomainNameConfiguration.TargetDomainName().Elem()
	if zone != "" {
		_, e = route53.NewRecord(ctx, "index-hostname", &route53.RecordArgs{ZoneId: pulumi.String(zone), Name: pulumi.String(domain), Type: pulumi.String("CNAME"), Ttl: pulumi.Int(300), Records: pulumi.StringArray{target}}, awslabels.Option(ctx))
		if e != nil {
			return e
		}
	}
	ctx.Export("apiUrl", api.ApiEndpoint)
	ctx.Export("apiArn", api.ExecutionArn)
	ctx.Export("indexUrl", pulumi.String(fmt.Sprintf("https://%s/v1/environments", domain)))
	ctx.Export("domainTarget", target)
	ctx.Export("tableName", table.Name)
	ctx.Export("sourceHash", pulumi.String(cfg.Require("sourceHash")))
	return labels.Export(ctx, "resourceInventory")
}

// Validate the whole shared configuration before either admin or index native
// provisioning begins. Errors identify the setting without exposing its value.
func validateSharedConfiguration(cfg *config.Config) error {
	for _, key := range []string{"domain", "adminDomain", "sourceHash"} {
		if strings.TrimSpace(cfg.Get(key)) == "" {
			return fmt.Errorf("shared configuration: %s is required", key)
		}
	}
	for _, key := range []string{"domain", "adminDomain"} {
		if !validDNSHostname(cfg.Get(key)) {
			return fmt.Errorf("shared configuration: %s must be a DNS hostname", key)
		}
	}
	if strings.EqualFold(cfg.Get("domain"), cfg.Get("adminDomain")) {
		return fmt.Errorf("shared configuration: domain and adminDomain must be distinct")
	}
	return nil
}

func validDNSHostname(host string) bool {
	if len(host) > 253 || net.ParseIP(host) != nil {
		return false
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
