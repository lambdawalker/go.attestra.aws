# Migration and deployment boundary

This refactor separates source ownership. It does not authorize or execute a live migration. Legacy source is pinned to go.attestra.aws.auth commit 4607807ab0346f4a38532982889fe2e56954412e. Authentication now lives in go.apexfission.aws.auth, while this application owns composition and its control plane.

## What is preserved

Email two-token/manual confirmation, Cognito/passkeys/password recovery, capture/extraction, runtime onboarding contracts, consent/revision/grant behavior, admin UI/API and index publication follow the extracted implementations and tests. Imports and ownership change; feature runtime is not duplicated in the application.

The application still builds Linux/ARM64 Lambda archives and uses S3/Pulumi/operator workflows with independently managed environments. Runtime table bindings remain deferred native Outputs inside a stack and explicit references across stacks.

## What changes operationally

| Boundary | Change |
|---|---|
| Go packages | Consumers/builds use pinned feature module package paths |
| Backend project | attestra-application, rather than attestra-auth-email |
| Shared project | attestra-control-plane, rather than attestra-index |
| Reset function | Application prefix attestra-app-auth-reset-, validated server-side and reflected in IAM |
| Proof key | Same caller-supplied bytes, delivered through pinned Secrets Manager version instead of raw Lambda environment |
| Source hashing | Application sources, internal transport and dependency manifests/checksums participate |
| Teardown | Component references are not AWS resources; secrets use metadata-only verification, with pending deletion reported as retained/recoverable |

A new project name changes Pulumi URNs. A new Cognito pool changes user identity/session context. New hostnames affect CORS, passkey RP ID, OAuth/client bootstrap and Android associations. A new runtime table does not automatically contain policy history, grants or consent data. None of those are solved by importing the feature.

## Reviewable rollout sequence

1. Review and merge feature/application code in dependency order. The application pins the exact feature commit, including its infrastructure module; no mutable branch lookup or sibling replace is needed.
2. Choose isolated test account/stack/backend and nonconflicting application/API/admin/index/sender domains. An alternate project alone does not prevent collisions in explicitly named AWS resources.
3. Provision/validate the new shared control plane, then explicitly bind runtimeTableName/runtimeTableArn for a test environment. Bootstrap remains an operator workflow; missing bindings fail instead of creating an unrelated replacement table.
4. Build pinned feature and application artifacts. Preview all replacements/imports, IAM/secret permissions, domain bindings and reset targets. Test against synthetic users/evidence.
5. Verify email delivery, passkeys/sign-in, capture disabled/enabled policy, consent changes, reset boundaries, admin access, shared publication and recovery. Native mocks are insufficient for these live checks.
6. Decide whether production uses deliberate resource adoption or a side-by-side data/client migration. Inventory current stack state, physical IDs, credential custody, data backups and in-flight work before either route.
7. Preserve existing proof key material for pending proofs; changing source need not change the key. Record secret version/reader/Lambda rollout. Rotation or revocation needs a separate compatibility/draining decision.
8. Cut over only after application/client availability and readiness gates. Retain old routes/resources and independent operator recovery for the agreed rollback window.
9. Retire legacy ownership deliberately after data/dependency/traffic checks. Do not run both Pulumi projects as owners of the same resource.

The existing --migrate-from flow is conservative and limited to its declared project/stack contract. It does not silently rewrite legacy URNs into this application or decrypt/import old credentials. State export/import, alias design, data copy, cross-account transfers and credential recovery require their own concrete reviewed plan.

## Standalone feature verification

The feature can create an isolated policy table without this admin/index. Its seed command consumes exported runtimeDefaults rather than inventing enabled defaults, validates the environment, and conditionally initializes once. Reruns preserve existing overrides.

## Secret recovery and teardown

No secret recovery window was shortened for the refactor. A secret scheduled for deletion is still recoverable; teardown reports retained metadata/deadline rather than claiming immediate absence. Do not force-delete it just to make verification green. Raw secret values are never retrieved for teardown probes.

No live deployment, reset, purge, teardown, state migration, package publication or production data request was used to validate these changes.
