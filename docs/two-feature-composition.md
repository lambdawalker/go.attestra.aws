# App plus two features

This integration branch composes authentication and isolated email verification through go.infra.composer. Main is unchanged until the branches are merged.

| Owner | Resources and behavior |
|---|---|
| Authentication | Cognito, custom grant challenge, grants/enrollment table (legacy logical name email-proofs), SES identity, passkeys, sign-in, capture, policy readers, auth reset contribution |
| Email verification | A+B/B+C protocol, three handlers, separate verification-proofs table, Secrets Manager key/version, scoped IAM and host API routes |
| Application | Shared runtime policy/control plane, explicit injection, configuration source decisions, API stage/domain coordination and final combined reset deployment |

The app injects authentication.email_integration into email-verification.identity_integration. The contract is identity.email-enrollment/v1 and all ten fields are validated before execution. Verification imports no auth code; neither feature imports the app. Both retain their independent manifests; the application combines them without copying handlers. There are 20 backend archives and one app admin archive, plus the separately built index bootstrap.

The application validates both configuration contracts and both archive sets before registering resources. Native outputs remain Pulumi outputs across the boundary. Reset is finalized after both groups: it pauses all 18 state-changing target functions (the read-only app-config handler stays outside the pause inventory) and clears both proof and grant tables; the reset worker itself is not a target. Mocks verify the exact manifest and scoped permissions. No new panel is deployed; app-owned configuration remains the default.

## Test locally

```sh
go run ./cmd/compose --environment qa --json
go test -race ./...
go vet ./...
```

Also run tests/race/vet in infra, infra-index and tools/deploy, then scripts/build.sh (or build.bat) and scripts/check_lambda_archives.py. Node admin tests and Python helper tests remain part of verification. These commands do not deploy AWS; the compose CLI's default runtime provider is a marked test fixture and is rejected in production environments.

## Private feature dependency

The verification repository is private. Local builds need GitHub read access (for example gh auth setup-git) and GOPRIVATE=github.com/lambdawalker/go.apexfission.aws.verifyemail. CI needs a FEATURES_READ_TOKEN Actions secret with Contents: read on that repository. The application's ordinary GITHUB_TOKEN cannot read another private repository. Workflow authentication is configured before module downloads; no token is stored in source. Do not change repository visibility to work around missing credentials.

The dependencies are immutable commit pins; no sibling clone or cross-repository replacement is required. Same-repository replacements in nested modules are only for local development.

## Migration boundary

Keep the existing deployment running while testing a separate environment/prefix. Existing proof transactions, budgets and in-flight confirmation state remain in the legacy table and old handlers; they are not transferred automatically. Drain them or define an explicit revision-safe migration before cutover. Authentication grants and enrollment facts retain their old table; new verification requests go to the separate proof table. Preserve the exact proof-key bytes for any supported old requests; pinned secret changes are not automatic rotation. API paths and JSON responses stay compatible. DNS, SES, users, sessions, state ownership and application reset permissions need a reviewed rollout. No AWS resources or data were changed by this refactor.
