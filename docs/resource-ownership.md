# Resource ownership on main

The main deployment uses the merged infra.composer ownership helpers. The environment backend and shared admin/index deployment apply canonical ic:application/environment/domain/feature/component/deployment/managed-by tags to resources supported by the provider schema, portable mutable descriptions and metadata-only resourceInventory outputs.

Main composes authentication and isolated email verification, preserving 20 backend Lambda archives. Both feature repositories are public; no feature-read token is required.

Environment resources use the selected environment and an application-origin hostname by default. Shared admin/index and state use environment shared and the shared hostname. Set ownershipDomain in the relevant Pulumi stack to choose an explicit hostname label. The labeling convention does not rename resources. See the [two-feature migration boundary](two-feature-composition.md#migration-boundary) before rolling out the verification extraction to an existing deployment. IAM managed-policy descriptions are not rewritten; legacy SES adapter resources are explicitly inventory-only.

The bootstrap reads and merges state-bucket tags, rejects conflicting ownership and labels its setup-owned deployment role. External account-wide OIDC/KMS resources are not claimed. Reconcile out-of-band operator tags through desired configuration or AWS provider ignoreTags before rollout. A reviewed preview is required before applying metadata changes to a live account.

Inspect the resourceInventory stack output for owned native identifiers and parent references. Inventory and tags aid discovery; they do not authorize deletion. See the [standard](https://github.com/lambdawalker/design.infra.composer/blob/main/docs/resource-ownership.md).

Descriptions use `domain ENV | owner`, with uppercase environment and the creating repository: `index.attestrabond.com SHARED | go.attestra.aws` and `dev.attestrabond.com DEV | go.apexfission.aws.auth`. Resource names and purposes are omitted from descriptions; tags and inventory keep their existing component details.
