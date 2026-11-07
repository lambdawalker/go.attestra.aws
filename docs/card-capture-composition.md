# Card capture feature composition

Attestra composes three public, pinned features: authentication, email verification, and [card capture](https://github.com/lambdawalker/go.appexfission.aws.card-capture). The application builds 20 backend Lambda archives: 14 authentication, 3 verification and 3 capture. Capture runtime, workers, S3/DynamoDB/SQS resources, OCR and capture routes live in their feature repository.

Capture declares storage separately from its runtime backend. Storage offers a versioned `capture.facts` table contract to authentication, which reads enrollment status without owning the table. Authentication then offers its API, JWT identity and reset gate via `identity.card-capture/v1` to capture's runtime. This storage → authentication → capture ordering avoids a creation cycle. The application injects shared runtime configuration into both features.

The standard-library-only capture `contracts` package defines consent policy fields, messages and hashing. Authentication uses that wire contract without importing the capture service or AWS implementation. Capture's runtime policy adapter reads the shared configuration document and its reset adapter reads only the token fence; it does not administer identity, runtime policy or environment reset.

The application finalizes one reset inventory after all three native deployments: capture table, versioned evidence bucket, both queues and all capture writer functions are included. Existing source logical resource names, API paths, consent versions, ownership inventory, concurrency preflight and recent immediate queue dispatch are retained. Capture descriptions identify `go.appexfission.aws.card-capture` as the creating repository. No runtime or infrastructure source is copied into the aggregator.

## Verification and rollout

Run tests/race/vet in all modules, build combined Lambda archives and check their contents. The native mock suite verifies a complete 20-function deployment, per-feature ownership and full reset coverage; missing capture or verification archives fail before any AWS resource registration. Test-fixture providers remain forbidden for production plans.

This extraction does not run an AWS deployment or migrate state. Inspect a Pulumi preview against the intended environment before rollout, preserving existing state/backend and data. Logical resource names are retained for adoption; investigate any proposed replacement. Existing API payloads and consent policy digests are preserved.

## Live OCR update and timing

The application pins Go auth and card capture in `go.mod`, `infra/go.mod` and
`tools/deploy/go.mod`. Keep the runtime and infrastructure selections aligned
when incorporating worker changes. The current source includes the worker
change to `medium` reasoning effort. The Android host still sends separate
color JPEGs.

After updating pins, run `./scripts/build.sh` and
`python3 scripts/check_lambda_archives.py`. The capture worker archive is
`dist/capture-worker.zip`. Extract its `bootstrap` and use `go version -m`
to check the resolved card-capture module before deploying the selected environment.
Follow the [deploy tool runbook](../tools/deploy/README.md) for the rollout.
Check the environment's deployment evidence before attributing a live request
to a source revision.

For a device smoke test, use the same photos before and after rollout. Confirm one
pair while processing and retake a side in another run. Check that the review uses
the latest image revision. Compare Android `CARD-OCR` timings with the matching
worker invocation. See [timing and route details](https://raw.githubusercontent.com/lambdawalker/go.appexfission.aws.card-capture/main/docs/agents/index.md),
[the Android recipe](https://raw.githubusercontent.com/lambdawalker/android.attestra.auth/main/docs/agents/recipes.md) and [the system journey](https://raw.githubusercontent.com/lambdawalker/design.attestra/main/docs/agents/journeys.md).
