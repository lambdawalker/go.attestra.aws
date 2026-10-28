# Attestra AWS application

An application consuming [Apexfission](https://github.com/lambdawalker/go.apexfission.aws.auth) through pinned Go dependencies. Authentication implementation stays in the library; this repository owns application composition, Lambda entrypoints, infrastructure, deployment configuration and its admin page.

Framework revision: [`8f7bdade14db`](https://github.com/lambdawalker/go.apexfission.aws.auth/commit/8f7bdade14db41377c3f695f9bc69519f4248ecc), the merged modular-framework implementation. Root, deployment and Pulumi modules are pinned to that commit. There are **no local replacements for the auth library**.

## Local setup

Install Go 1.26.6, then:

```sh
go test ./...
go run ./cmd/attestra build
go run ./cmd/attestra init
```

`build` creates executable Linux arm64 `dist/application.zip` and `dist/challenge.zip`. It works without a manifest or AWS credentials. `init` creates a private, ignored `apexfission.json` and refuses to overwrite an existing file. Set its real AWS account, region and S3 Pulumi state bucket before cloud commands:

```sh
go run ./cmd/attestra check --env dev
go run ./cmd/attestra preview --env dev
go run ./cmd/attestra deploy --env dev
go run ./cmd/attestra status --env dev
```

`check` only validates local configuration and linked modules. `preview` requires the infrastructure configuration in [deployment setup](docs/deployment.md). The project has not been deployed to AWS.

## Contents

| Path | Responsibility |
| --- | --- |
| `app` | Attestra module, protected self-profile route, admin page and build declarations |
| `cmd/api` | Framework customer auth API or dedicated admin shell, selected by deployment-owned MODE |
| `cmd/challenge` | Framework Cognito custom-auth trigger |
| `cmd/attestra` | Go application CLI wrapping the library's build/deployment engine |
| `internal/bootstrap` | Validated runtime settings and AWS adapter wiring |
| `infra` | Separate Go/Pulumi module composing the library's components |
| `.github/workflows` | Test/build CI and manually invoked OIDC preview/deploy |

The `infra` module's sole local replacement points to **this application's root**, so infrastructure can import `app` from the same checkout. It does not point to a sibling framework checkout. Lambda runtime packages do not import Pulumi.

## API

- `GET /health`: public process health. This does not verify SES delivery or provider availability.
- `GET /v1/me`: requires a verified Cognito access token and returns its subject.
- Framework routes: `/signup`, `/resend`, `/confirm`, `/passkeys/options`, `/passkeys/complete`, and `/auth/{email/start,email/complete,passkey/start,passkey/complete,refresh,status}`. These retain the framework's existing wire contracts.
- Admin: `/admin`, with OAuth PKCE through the separate invitation-only MFA pool. The Attestra page and `/admin/api/extensions/attestra/status` require `attestra:admin`.

Customer authorization grants only self-profile access; it cannot grant administrator privileges. ID capture is not enabled by this starter. No existing users/resources are imported or reset.

## Verification

```sh
go test -race ./...
go vet ./...
cd infra
go test -race ./...
go vet ./...
```

Tests exercise actual framework composition, default access denial, subject extraction, administrator separation, settings validation, safe initialization, placeholder guards and Pulumi registration. Infrastructure tests use mocks, not live AWS. See [verification](docs/verification.md).
