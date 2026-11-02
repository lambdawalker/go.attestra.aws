package main

import (
	"fmt"
	"github.com/lambdawalker/go.infra.composer/adapters/pulumi/awslabels"
	"github.com/lambdawalker/go.infra.composer/ownership"
	"net/url"
	"path/filepath"
	"strings"

	auth "github.com/lambdawalker/go.apexfission.aws.auth/feature"
	authinfra "github.com/lambdawalker/go.apexfission.aws.auth/infrastructure"
	application "github.com/lambdawalker/go.attestra.aws"
	"github.com/lambdawalker/go.attestra.aws/adminapi"
	composer "github.com/lambdawalker/go.infra.composer"
	adapter "github.com/lambdawalker/go.infra.composer/adapters/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() { pulumi.Run(deploy) }

type runtimeReference struct{ pulumi.ResourceState }

func stringInput(input pulumi.Input) pulumi.StringOutput {
	return pulumi.ToOutput(input).ApplyT(func(v any) (string, error) {
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("runtime binding must resolve to a string")
		}
		return s, nil
	}).(pulumi.StringOutput)
}
func deploy(ctx *pulumi.Context) error {
	cfg := config.New(ctx, "")
	binding := application.RuntimeBinding{TableName: cfg.Get("runtimeTableName"), TableARN: cfg.Get("runtimeTableArn")}
	app, err := application.New(binding)
	if err != nil {
		return err
	}
	env := composer.Environment{Name: ctx.Stack(), Region: config.New(ctx, "aws").Get("region"), Production: cfg.Get("environmentClass") == "production"}
	plan, err := app.Plan(env)
	if err != nil {
		return err
	}
	_, err = application.AuthenticationConfiguration(env.Name, application.AuthSettings{AppOrigin: cfg.Get("appOrigin"), SenderAddress: cfg.Get("senderAddress"), DiagnosticMode: cfg.Get("diagnosticMode") == "true", CaptureEnabled: cfg.Get("captureEnabled") != "false"})
	if err != nil {
		return err
	}
	dir := cfg.Get("artifactDir")
	if dir == "" {
		dir, err = filepath.Abs("../dist")
		if err != nil {
			return err
		}
	}
	resetName, err := adminapi.ResetFunctionName(adminapi.ResetFunctionPrefix, env.Name)
	if err != nil {
		return err
	}
	opts := authinfra.Options{ArtifactDir: dir, RuntimeTableName: pulumi.String(binding.TableName), RuntimeTableARN: pulumi.String(binding.TableARN), Environment: env.Name, ResetFunctionName: resetName}
	// Complete native preflight before even the reference component is registered.
	if err := authinfra.Validate(cfg, opts); err != nil {
		return err
	}
	domain := cfg.Get("ownershipDomain")
	if domain == "" {
		origin, _ := url.Parse(cfg.Get("appOrigin"))
		domain = strings.ToLower(origin.Hostname())
	}
	ctx, labels, err := awslabels.Install(ctx, ownership.Metadata{Owner: "go.attestra.aws", Application: "attestra", Environment: env.Name, Domain: domain, Feature: "application", Deployment: "attestra-" + env.Name})
	if err != nil {
		return err
	}
	registry := adapter.Registry{
		application.RuntimeKind: func(ctx *pulumi.Context, name string, inputs pulumi.Map, options ...pulumi.ResourceOption) (adapter.Registered, error) {
			state := &runtimeReference{}
			if err := ctx.RegisterComponentResource(application.RuntimeKind, name, state, options...); err != nil {
				return adapter.Registered{}, err
			}
			if err := ctx.RegisterResourceOutputs(state, inputs); err != nil {
				return adapter.Registered{}, err
			}
			return adapter.Registered{Resource: state, Outputs: map[string]pulumi.Output{"table_name": stringInput(inputs["table_name"]), "table_arn": stringInput(inputs["table_arn"])}}, nil
		},
		auth.Kind: func(ctx *pulumi.Context, name string, inputs pulumi.Map, _ ...pulumi.ResourceOption) (adapter.Registered, error) {
			settings := opts
			settings.RuntimeTableName = stringInput(inputs["runtime_table_name"])
			settings.RuntimeTableARN = stringInput(inputs["runtime_table_arn"])
			scoped, err := awslabels.ForFeature(ctx, application.AuthenticationID)
			if err != nil {
				return adapter.Registered{}, err
			}
			backend, err := authinfra.Deploy(scoped, cfg, settings)
			if err != nil {
				return adapter.Registered{}, err
			}
			return adapter.Registered{Resource: backend.Stage, Outputs: map[string]pulumi.Output{"api_url": backend.APIURL, "pool_id": backend.PoolID, "client_id": backend.ClientID, "issuer": backend.Issuer, "reset_function_name": backend.ResetFunctionName}}, nil
		},
	}
	if _, err := adapter.Execute(ctx, plan, registry); err != nil {
		return err
	}
	if err := labels.Export(ctx, "resourceInventory"); err != nil {
		return err
	}
	ctx.Export("sourceHash", pulumi.String(cfg.Get("sourceHash")))
	return nil
}
