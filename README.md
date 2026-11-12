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

Add `-timed-confirm` to setup/deploy for routine approvals that continue automatically after five seconds unless cancelled. See [timed deployment confirmations](tools/deploy/README.md#timed-deployment-confirmations).

All-environment teardown is available through `deploy.bat -teardown -nuke` / `./deploy.sh -teardown -nuke`, using Composer's shared teardown engine. It removes environment stacks before shared admin/index, persists verification/retry inventory, and gates state-bucket/KMS recovery cleanup on verified earlier success. Add `-check` for read-only verification or `-fast-check` for only the artifacts last checked as remaining. See [nuke scope, reporting and recovery limits](tools/deploy/README.md#all-environment-nuke) before running it.

## Migration boundary

The legacy [go.attestra.aws.auth](https://github.com/lambdawalker/go.attestra.aws.auth) remains operational. New project/state names, feature packaging, reset function prefix and secret delivery mean this is not an automatic in-place state move. Existing users, Cognito IDs, data and sessions do not transfer by changing imports.

Read [migration and deployment](docs/migration.md) before using a live target. No deployed stack files or credentials are checked in. Modules are pinned to immutable implementation commits; version tags and production release status are not implied.

## Authentication and email verification

The application composes [isolated email verification](https://github.com/lambdawalker/go.apexfission.aws.verifyemail). See [composition and testing](docs/two-feature-composition.md). Both feature manifests are built from pinned module dependencies, and application reset includes verification writers and its separate proof table.
## Deployment resource labels

Main deploys ownership tags, safe descriptions and resource inventory for the environment backend and shared admin/index. See [ownership and rollout](docs/resource-ownership.md).


## Card capture feature

Card capture is composed independently from [go.appexfission.aws.card-capture](https://github.com/lambdawalker/go.appexfission.aws.card-capture), including its three Lambda archives and native resources. See [contracts, reset composition, rollout and OCR timing](docs/card-capture-composition.md).

### Artifact reports and read-only preview

Every deployment attempt (backend, shared index/admin, full setup, and CI) writes
`deployment-reports/<UTC timestamp>-<project>/artifacts.md`, `artifacts.json`, and
`artifacts.html`. A partial bundle is written before deployment; completed native
updates receive fresh per-resource read-only checks even when `pulumi up` fails.
The final bundle also includes GitHub environments, Cloudflare DNS, the state
bucket, and the setup deployment role. Runtime user uploads are excluded. Reports
and scoped machine snapshots are private local files and ignored by Git. The HTML view includes a left deployment/feature sidebar, compact collapsible artifact cards, colored statuses, type icons and sorting controls. Subartifacts stay nested when sorting. CI keeps
only the safe report bundles as an artifact, including on failure.

On Windows, from the repository root:

```powershell
.\deploy.bat -report -stack dev
.\deploy.bat -report -scope both -stack dev
.\deploy.bat -report -stack dev -account 123456789012 -backend s3://my-state-bucket
.\deploy.bat -report -stack dev -feature authentication
.\deploy.bat -report -refresh -stack dev
.\deploy.bat -preview -stack dev
.\deploy.bat -preview -scope shared
```

Use `./deploy.sh` with the same flags on macOS/Linux. `-env dev` is an alias for
`-stack dev` in reporting modes. `-scope` accepts `app`, `shared`, or `both`.
`-account`, `-backend`, and `-region` narrow cached scope selection. Multiple
matching accounts, backends, repositories, or stacks are refused rather than
selected arbitrarily. `both` requires one cached snapshot for each project.

`-report` runs before the startup menu and needs no AWS/Pulumi credentials,
provider tools, or network. It renders the stored snapshot and preserves the
original observation times and sources; `cacheLoaded` identifies loaded evidence.
`-feature` selects an exact canonical feature and retains its parent hierarchy.

`-report -refresh` and `-preview` use the configured AWS credential chain (exported
AWS credentials or `AWS_PROFILE`) and `PULUMI_CONFIG_PASSPHRASE`. They do not unlock
the local vault or request extra tokens. First authenticate with your normal AWS
login/SSO flow and set the stack passphrase in the current terminal. The AWS STS
account must match the selected snapshot; the project, backend and stack are
explicit. For a first preview without a cache, supply `-backend`, `-region`, and
optionally `-account`. Keep passphrases out of command arguments and scripts.

`-preview` runs native Pulumi preview and captures its planned operations without
`up`, registry publication, bootstrap locks, or authentication reconciliation.
Existing `-ci preview` also writes reports. Preview does not replace a live cache
with an offline plan. Reporting operation flags cannot be combined with setup,
teardown, migration, publication, health checks, or deployment operation flags.

A state record means **recorded**, not live present. A successful exact provider
check means **present** or **absent**; permission errors, missing credentials,
unsupported checks, and incomplete child checks mean **unknown**. Parent presence
does not prove child presence. DNS with a different target stays present and adds
a configuration mismatch warning. A successful infrastructure update followed by
a failed health/reconciliation step has a separate outcome. Cached snapshots live
under `.cache/infra-composer/`, scoped by application, repository, environment,
AWS account, region, backend, Pulumi project and stack.

Normal interactive deployments reuse already unlocked vault GitHub/Cloudflare
tokens. CI and read-only refresh can use `GH_TOKEN` (or `GITHUB_TOKEN`) and
`CLOUDFLARE_API_TOKEN`. Missing supplemental tokens produce unknown observations;
no extra token prompts are introduced. Raw state exports, engine events,
provider diagnostics, credentials and runtime S3 contents are never published.

### Upgrading existing email verification routes

The pinned email feature includes aliases from the original `route-signup`,
`route-resend`, and `route-confirm` Pulumi resources. This fixes API Gateway 409
conflicts when upgrading a pre-extraction stack. After updating the checkout,
run `deploy.bat -preview -stack dev` with the documented preview credentials and
verify those routes are adopted rather than created before deploying. Avoid
timed approvals for this migration review. Do not delete the existing routes or
remove their state entries to work around a conflict. If they are absent from
state, inspect the stack and API identities before choosing an import/recovery.

### Read-only dashboard artifact explorer

The authenticated console opens on the latest saved deployment snapshot. Composer renders artifact cards, hierarchy, filters, sorting and allowlisted recorded configuration inside an isolated iframe. Reload reads saved evidence and does not scan AWS. Runtime configuration remains a separate navigation choice. See [rollout, publication, limits and example report](docs/artifact-explorer.md). Existing environments require a full setup rerun to upgrade the shared UI and grant the exact snapshot PutItem permission, followed by a complete application deployment to publish the first snapshot.

## Migration and compatibility debt

See [tech-debt.md](tech-debt.md) for the cross-repository register of migration bridges, legacy data contracts, and the evidence required before removing them.

### Document review rollout (Development)

The capture runtime dependency now includes document type/name, issuing authority
and country/region/city issuing jurisdiction. Matching Android review changes are
in [auth PR #60](https://github.com/lambdawalker/android.apexfission.auth/pull/60);
the owning backend contract is in
[capture PR #45](https://github.com/lambdawalker/go.appexfission.aws.card-capture/pull/45).
The root runtime, infrastructure and deploy-tool manifests pin the same feature
revision. Deploy this host revision before using Android's new review fields;
older strict servers reject unknown keys. This source change does not deploy AWS.
