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
	verify "github.com/lambdawalker/go.apexfission.aws.verifyemail/feature"
	verifyinfra "github.com/lambdawalker/go.apexfission.aws.verifyemail/infrastructure"
	card "github.com/lambdawalker/go.appexfission.aws.card-capture/feature"
	cardinfra "github.com/lambdawalker/go.appexfission.aws.card-capture/infrastructure"
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
	opts := authinfra.Options{DeferReset: true, ArtifactDir: dir, RuntimeTableName: pulumi.String(binding.TableName), RuntimeTableARN: pulumi.String(binding.TableARN), Environment: env.Name, ResetFunctionName: resetName}
	// Complete native preflight before even the reference component is registered.
	if err := authinfra.Validate(cfg, opts); err != nil {
		return err
	}
	verificationOpts := verifyinfra.PreflightOptions(dir, env.Name)
	if err := verifyinfra.Validate(cfg, verificationOpts); err != nil {
		return err
	}
	if _, err := application.EmailVerificationConfiguration(env.Name, application.AuthSettings{AppOrigin: cfg.Get("appOrigin"), SenderAddress: cfg.Get("senderAddress"), DiagnosticMode: cfg.Get("diagnosticMode") == "true", CaptureEnabled: cfg.Get("captureEnabled") != "false"}); err != nil {
		return err
	}
	captureOpts := cardinfra.Options{ArtifactDir: dir, Environment: env.Name, RuntimeTableName: pulumi.String(binding.TableName), RuntimeTableARN: pulumi.String(binding.TableARN)}
	if err := cardinfra.ValidateStorage(cfg, captureOpts); err != nil {
		return err
	}
	if _, err := application.CardCaptureConfiguration(env.Name, application.AuthSettings{AppOrigin: cfg.Get("appOrigin"), SenderAddress: cfg.Get("senderAddress"), DiagnosticMode: cfg.Get("diagnosticMode") == "true", CaptureEnabled: cfg.Get("captureEnabled") != "false"}); err != nil {
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
	var authentication *authinfra.Deployment
	var verification *verifyinfra.Deployment
	var captureStorage *cardinfra.Storage
	var captureBackend *cardinfra.Deployment
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
			settings.CaptureTableName = stringInput(inputs["capture_table_name"])
			settings.CaptureTableARN = stringInput(inputs["capture_table_arn"])
			scoped, err := awslabels.ForFeature(ctx, application.AuthenticationID)
			if err != nil {
				return adapter.Registered{}, err
			}
			backend, err := authinfra.Deploy(scoped, cfg, settings)
			if err != nil {
				return adapter.Registered{}, err
			}
			authentication = backend
			outputs := map[string]pulumi.Output{"api_url": backend.APIURL, "pool_id": backend.PoolID, "client_id": backend.ClientID, "issuer": backend.Issuer, "reset_function_name": backend.ResetFunctionName}
			for k, v := range backend.CaptureIntegration {
				outputs[k] = v
			}
			for k, v := range backend.Integration {
				outputs[k] = v
			}
			return adapter.Registered{Resource: backend.Stage, Outputs: outputs}, nil
		},
		card.KindStorage: func(ctx *pulumi.Context, name string, inputs pulumi.Map, _ ...pulumi.ResourceOption) (adapter.Registered, error) {
			scoped, err := awslabels.ForFeature(ctx, application.CardCaptureID)
			if err != nil {
				return adapter.Registered{}, err
			}
			storage, err := cardinfra.DeployStorage(scoped, cfg, captureOpts)
			if err != nil {
				return adapter.Registered{}, err
			}
			captureStorage = storage
			return adapter.Registered{Resource: storage.Table, Outputs: map[string]pulumi.Output{
				"table_name": storage.Table.Name, "table_arn": storage.Table.Arn,
				"bucket_name": storage.Bucket.ID(), "bucket_arn": storage.Bucket.Arn,
				"queue_url": storage.Queue.Url, "queue_arn": storage.Queue.Arn,
				"dead_letter_queue_url": storage.DeadLetterQueue.Url, "dead_letter_queue_arn": storage.DeadLetterQueue.Arn,
			}}, nil
		},
		card.Kind: func(ctx *pulumi.Context, name string, inputs pulumi.Map, _ ...pulumi.ResourceOption) (adapter.Registered, error) {
			if captureStorage == nil {
				return adapter.Registered{}, fmt.Errorf("capture storage not registered")
			}
			settings := captureOpts
			settings.RuntimeTableName = stringInput(inputs["runtime_table_name"])
			settings.RuntimeTableARN = stringInput(inputs["runtime_table_arn"])
			settings.Integration = map[string]pulumi.StringInput{}
			for k := range card.HostIntegrationContract().Fields {
				settings.Integration[k] = stringInput(inputs[k])
			}
			scoped, err := awslabels.ForFeature(ctx, application.CardCaptureID)
			if err != nil {
				return adapter.Registered{}, err
			}
			backend, err := cardinfra.Deploy(scoped, cfg, settings, captureStorage)
			if err != nil {
				return adapter.Registered{}, err
			}
			captureBackend = backend
			return adapter.Registered{Resource: backend.Functions["capture"], Outputs: map[string]pulumi.Output{
				"capture_function_name":    backend.Functions["capture"].Name,
				"worker_function_name":     backend.Functions["capture-worker"].Name,
				"dispatcher_function_name": backend.Functions["capture-dispatcher"].Name,
			}}, nil
		},
		verify.Kind: func(ctx *pulumi.Context, name string, inputs pulumi.Map, _ ...pulumi.ResourceOption) (adapter.Registered, error) {
			settings := verificationOpts
			settings.Integration = map[string]pulumi.StringInput{}
			for k := range verify.IntegrationContract().Fields {
				settings.Integration[k] = stringInput(inputs[k])
			}
			scoped, err := awslabels.ForFeature(ctx, application.EmailVerificationID)
			if err != nil {
				return adapter.Registered{}, err
			}
			backend, err := verifyinfra.Deploy(scoped, cfg, settings)
			if err != nil {
				return adapter.Registered{}, err
			}
			verification = backend
			return adapter.Registered{Resource: backend.Table, Outputs: map[string]pulumi.Output{"proof_table_name": backend.Table.Name, "proof_table_arn": backend.Table.Arn}}, nil
		},
	}
	if _, err := adapter.Execute(ctx, plan, registry); err != nil {
		return err
	}
	if authentication == nil || verification == nil || captureBackend == nil {
		return fmt.Errorf("composition did not register all three features")
	}
	inventory := authentication.ResetInventory
	inventory.Tables = append(inventory.Tables, verification.Table.Name)
	inventory.TableARNs = append(inventory.TableARNs, verification.Table.Arn)
	for _, fn := range verification.Functions {
		inventory.Functions = append(inventory.Functions, fn.Name)
		inventory.FunctionARNs = append(inventory.FunctionARNs, fn.Arn)
	}
	inventory.CaptureTable = captureStorage.Table.Name
	inventory.CaptureTableARN = captureStorage.Table.Arn
	inventory.Tables = append(inventory.Tables, captureStorage.Table.Name)
	inventory.TableARNs = append(inventory.TableARNs, captureStorage.Table.Arn)
	inventory.Bucket = captureStorage.Bucket.ID()
	inventory.BucketARN = captureStorage.Bucket.Arn
	inventory.Queues = append(inventory.Queues, captureStorage.Queue.Url, captureStorage.DeadLetterQueue.Url)
	inventory.QueueARNs = append(inventory.QueueARNs, captureStorage.Queue.Arn, captureStorage.DeadLetterQueue.Arn)
	for _, fn := range captureBackend.Functions {
		inventory.Functions = append(inventory.Functions, fn.Name)
		inventory.FunctionARNs = append(inventory.FunctionARNs, fn.Arn)
	}
	if _, err := authinfra.FinalizeReset(ctx, cfg, opts, inventory); err != nil {
		return err
	}
	ctx.Export("verificationProofTable", verification.Table.Name)
	if err := labels.Export(ctx, "resourceInventory"); err != nil {
		return err
	}
	ctx.Export("sourceHash", pulumi.String(cfg.Get("sourceHash")))
	return nil
}
