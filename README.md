# go.attestra.aws

Application composition and deployment for Attestra. Authentication is the independently versioned [go.apexfission.aws.auth](https://github.com/lambdawalker/go.apexfission.aws.auth) feature; this repository does not contain a second copy of its runtime.

**Status:** executable source integration using [go.infra.composer](https://github.com/lambdawalker/go.infra.composer), pinned Go modules and native Pulumi factories. No AWS deployment, user/data migration, credential transfer or live routing change was performed. Go 1.27.3+.

## Ownership

| Owner | Responsibility |
|---|---|
| Authentication feature | Email proof, passkey/password sign-in, ID capture/extraction, auth runtime policy/reset, 20 Lambda artifacts and native infrastructure |
| Application | Admin/index UI and identities, shared runtime/registry, environments, source bindings, deployment memory/SSO/OIDC, artifact builds, readiness/publication and operator workflows |
| Composer | Contract resolution, injected resource-group prerequisites, diagnostics, immutable plans and Pulumi adapter boundary |

The application owns integrated configuration by default. A feature panel is not deployed. Existing admin grants, consent/history, policy revisions and public discovery allowlists remain in their original code paths.

## Inspect composition locally

```sh
go run ./cmd/compose -environment dev
go run ./cmd/compose -environment qa -json
go test -race ./...
go vet ./...
node --test adminapi/web_test.cjs
```

Without table arguments the command uses an explicitly test-only local binding; production rejects it. Supply `-runtime-table` and `-runtime-arn` for metadata describing an actual application-owned table. This command validates declarations only and never connects to AWS.

`application.New(binding, additionalFeatures...)` registers authentication and injects the application runtime contract. Additional features can require `auth.identity/v1` or `auth.api/v1` without importing another feature's implementation. Their native resource factories belong in the application's bounded registry. The current native authentication adapter supports one auth backend per stack.

## Deployment modules

- `infra-index`: application-owned shared admin/index and protected runtime table; Pulumi project `attestra-control-plane`.
- `infra`: application backend composition and native authentication factory; project `attestra-application`.
- `tools/deploy`: encrypted operator memory, setup, SSO/OIDC, preview/deploy, health/publication and teardown.

The native backend program validates composer bindings, supported configuration sources and all feature settings/archives before registering resources. The shared program checks all required hostnames/settings before AWS constructors. Shared state is deployed separately; environment programs bind its outputs without re-owning the table.

Configuration contract metadata distinguishes Application settings from Artifact settings; category does not grant permission. `AuthenticationConfiguration` records the supported delivery policy. Its pinned secret reader does not promise one-second fleet rotation: it checks an exact immutable secret version; changing that binding requires a deployment. Runtime onboarding overrides remain authoritative in the shared transactional store.

## Builds and verification

```sh
./scripts/build.sh
python scripts/check_lambda_archives.py
```

Windows uses `scripts/build.bat`. The Go builder consumes the feature artifact manifest through the pinned module dependency, builds nine sign-in routes once, and builds the local admin. The index archive is built by the shared workflow. No sibling feature checkout is required. The root `go.mod` also declares each unique feature Lambda command as a Go tool dependency so `go mod tidy` retains its runtime dependencies and checksums. When upgrading the feature manifest, keep this tool list in sync.

Run tests/vet separately in `infra`, `infra-index` and `tools/deploy`. CI also checks native mocks, archive shape, dashboard interactions and dependency integrity. Provider mocks do not prove live IAM, DNS, model availability or data migration.

Setup/deploy entry points remain `setup.sh`/`setup.bat` and `deploy.sh`/`deploy.bat`. Preview is available through the existing operation menu. These commands are real operator actions: inspect the selected account, region, environment, project/backend and domains before authorizing execution. They are not validation commands for this refactor.

All-environment teardown is available through `deploy.bat -teardown -nuke` / `./deploy.sh -teardown -nuke`, using Composer's shared teardown engine. It removes environment stacks before shared admin/index, persists verification/retry inventory, and gates state-bucket/KMS recovery cleanup on verified earlier success. Add `-check` for read-only verification or `-fast-check` for only the artifacts last checked as remaining. See [nuke scope, reporting and recovery limits](tools/deploy/README.md#all-environment-nuke) before running it.

## Migration boundary

The legacy [go.attestra.aws.auth](https://github.com/lambdawalker/go.attestra.aws.auth) remains operational. New project/state names, feature packaging, reset function prefix and secret delivery mean this is not an automatic in-place state move. Existing users, Cognito IDs, data and sessions do not transfer by changing imports.

Read [migration and deployment](docs/migration.md) before using a live target. No deployed stack files or credentials are checked in. Modules are pinned to immutable implementation commits; version tags and production release status are not implied.

## Authentication and email verification

The application composes [isolated email verification](https://github.com/lambdawalker/go.apexfission.aws.verifyemail). See [composition and testing](docs/two-feature-composition.md). Both feature manifests are built from pinned module dependencies, and application reset includes verification writers and its separate proof table.
## Deployment resource labels

Main deploys ownership tags, safe descriptions and resource inventory for the environment backend and shared admin/index. See [ownership and rollout](docs/resource-ownership.md).

