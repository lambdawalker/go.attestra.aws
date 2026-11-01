# Application ownership

This repository composes features; authentication runtime and native resources belong to the pinned go.apexfission.aws.auth module. Do not copy feature runtime packages into this application or require sibling clones.

Run Go tests/vet in root, infra, infra-index and tools/deploy, plus Node admin interaction tests and archive/helper checks for changed code. Use mocks/fakes for provider integration. Never deploy, migrate live state, teardown, seed production data or publish packages to validate a refactor. Keep stack settings, credentials, receipts and generated archives out of Git.
