# Card capture feature composition

Attestra composes three public, pinned features: authentication, email verification, and [card capture](https://github.com/lambdawalker/go.appexfission.aws.card-capture). The application builds 20 backend Lambda archives: 14 authentication, 3 verification and 3 capture. Capture runtime, workers, S3/DynamoDB/SQS resources, OCR and capture routes live in their feature repository.

Capture declares storage separately from its runtime backend. Storage offers a versioned `capture.facts` table contract to authentication, which reads enrollment status without owning the table. Authentication then offers its API, JWT identity and reset gate via `identity.card-capture/v1` to capture's runtime. This storage → authentication → capture ordering avoids a creation cycle. The application injects shared runtime configuration into both features.

The standard-library-only capture `contracts` package defines consent policy fields, messages and hashing. Authentication uses that wire contract without importing the capture service or AWS implementation. Capture's runtime policy adapter reads the shared configuration document and its reset adapter reads only the token fence; it does not administer identity, runtime policy or environment reset.

The application finalizes one reset inventory after all three native deployments: capture table, versioned evidence bucket, both queues and all capture writer functions are included. Existing source logical resource names, API paths, consent versions, ownership inventory, concurrency preflight and recent immediate queue dispatch are retained. Capture descriptions identify `go.appexfission.aws.card-capture` as the creating repository. No runtime or infrastructure source is copied into the aggregator.

## Verification and rollout

Run tests/race/vet in all modules, build combined Lambda archives and check their contents. The native mock suite verifies a complete 20-function deployment, per-feature ownership and full reset coverage; missing capture or verification archives fail before any AWS resource registration. Test-fixture providers remain forbidden for production plans.

This extraction does not run an AWS deployment or migrate state. Inspect a Pulumi preview against the intended environment before rollout, preserving existing state/backend and data. Logical resource names are retained for adoption; investigate any proposed replacement. Existing API payloads and consent policy digests are preserved.
