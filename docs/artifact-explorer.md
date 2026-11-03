# Artifact explorer

The authenticated environment dashboard opens on **Artifacts**. Runtime configuration, passkeys, grants and reset retain their existing controls in the separate Runtime configuration view. Artifact inventory and recorded settings are read only. All active environment grantees may read that environment's saved report; the public environment directory never exposes it.

The report is the latest complete application deployment attempt that successfully published a snapshot. It can describe a failed deployment or unknown observations. Reload reads the existing saved report; it does not run an AWS scan. The snapshot completion timestamp and each original observation timestamp remain visible. Presence can change after observation. Recorded low-level configuration comes only from allowlisted Pulumi state outputs and retains its independent state-read timestamp; it is not live configuration verification. Composer supplies hierarchy, filters, sorting and collapsible cards. Download whole snapshot JSON ignores display filters and fetches the latest saved whole snapshot at download time.

## Rollout

Use the existing setup/deploy prerequisites: Go 1.27.3+, AWS CLI, Pulumi, authorized AWS credentials, the original application/shared backend passphrases, and the configured DNS/GitHub access for setup. Keep passphrases and credentials out of command arguments. Review each infrastructure preview through the normal workflow.

For a new environment, finish setup first, then run a complete application deployment. For existing environments, update the checkout and rerun the full setup wizard for each selected environment:

```sh
./setup.sh -stack dev -force-setup
./deploy.sh -stack dev
```

Windows equivalents are `.\setup.bat -Stack dev -force-setup` and `.\deploy.bat -stack dev`. Full setup upgrades the shared admin/index code when its source hash changes, persists validated `runtimeTableName` / `runtimeTableArn` outputs into the application configuration, and adds the `attestra-artifact-snapshot` inline policy to that environment's setup-managed GitHub deployment role. The policy permits only `dynamodb:PutItem` on the exact shared runtime table ARN, with `ForAllValues:StringEquals` and `Null:false` LeadingKeys restrictions for `ENV#dev`. Existing deployment roles do not gain this permission merely by updating source; rerun full setup. IAM-only maintenance does not add the snapshot policy without the shared outputs supplied by full setup.

To update just the shared dashboard separately, use the normal shared deployment workflow with the original shared passphrase:

```sh
./deploy.sh -scope shared
```

A shared UI upgrade alone publishes no application snapshot and does not update the application deployment role. A subsequent complete application deployment is required. Until one publishes successfully, the UI explicitly says that no snapshot exists. This change targets the current shared authenticated console and does not migrate the pending environment-bound admin branch; its guarded routes can reuse the same reader and UI later.

## Publishing and limits

A real, untargeted application `pulumi up` attempt writes a typed Composer report only after the deployment's state read and fresh checks finish. Failed attempts can publish their actual outcome and unknown evidence. Preview, local `-report`, `-report -refresh`, targeted/excluded partial updates, shared deployments and preflight-only attempts never publish. If the current post-update state cannot be read, remote publication is skipped and the prior snapshot remains available.

The deployment invokes local `cmd/artifact-snapshot` through the existing isolated, renewable SDK credential environment. The helper is a host command, never a Lambda. A private temporary report file carries the typed report; its contents and credentials are absent from command arguments and logs. Publication failure adds the stable local `snapshot_publish_failed` warning and preserves the original deployment result, local report bundle and cache. Local files still describe the attempt even when the remote snapshot is older. Check their warnings when diagnosing snapshot age.

The existing runtime table stores one item at `id=ENV#<environment>`, `sk=ARTIFACT_REPORT`: a gzip binary `payload` and numeric `finished` Unix-nanosecond timestamp. Conditional writes require the new completion timestamp to be strictly later; stale and equal writes are harmless explicit `stale` results. No extra table or scheduler is created. Limits are 256 KiB compressed and 512 KiB typed JSON; oversized reports are rejected whole, never truncated. Unsupported schema, missing metadata, wrong scope, unknown raw JSON fields and corrupt payloads are rejected. Dashboard reads are strongly consistent and set `cacheLoaded` without changing source observations.

The guarded GET endpoints are `/api/environments/{environment}/artifacts` (JSON) and `/api/environments/{environment}/artifacts-report` (Composer HTML). Existing issuer, client, access-token and environment grant checks run before any snapshot read. There is no artifact mutation endpoint or dashboard writer interface. Missing reports return coded `missing_snapshot` (404); corrupt/unavailable reports return distinct safe 503 codes. HTML exceeding 2 MiB returns `report_too_large` (413) while bounded JSON remains downloadable. Responses use `no-store` and `nosniff`.

The UI fetches HTML with authentication and creates a local blob iframe with `sandbox="allow-scripts"` and no same-origin permission. Exact SHA-256 hashes of the pinned Composer renderer's static style/script blocks allow its filtering under inherited CSP without enabling arbitrary inline code. Tokens never appear in iframe URLs, reports or browser storage. Environment changes, sign-out, expiry and artifact authorization failures clear report content, revoke blob/download URLs and invalidate pending responses.

An [example report](artifact-explorer-example.html) is generated from the nonsecret snapshot test fixture. It is illustrative, not deployed state. Validation uses fake SDK/CLI integrations, native infrastructure mocks and Node interaction tests; no AWS deployment or live scan is implied.
