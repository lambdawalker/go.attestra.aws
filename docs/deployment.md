# New-environment deployment

This project creates a new Attestra application stack. It does **not** reuse or modify the older `go.attestra.aws.auth` application's resources or state. Review a separate migration plan before adopting existing users or resources.

## Prerequisites

- Go 1.26.6, AWS CLI v2 and Pulumi CLI.
- An AWS profile/SSO session or CI OIDC role for the intended account.
- A private, encrypted, versioned S3 bucket for this application's Pulumi state.
- An application HTTPS origin, sender domain/address, and separate admin HTTPS hostname. Use a region supporting in-region Cognito/SES (the example uses us-east-1).
- An issued ACM certificate for the admin hostname in the deployment region. Infrastructure creates its API Gateway custom domain/mapping; you publish the DNS record afterward.

No account IDs, existing domains, credentials, invitations or grants were inferred or changed during setup.

## Configure locally

From the repository root:

```sh
go run ./cmd/attestra init
```

Edit ignored `apexfission.json`: replace the all-zero account and `replace-...` backend, choose your region, and keep each environment key equal to its stack name. Production environments must set `production: true`. The CLI rejects unchanged placeholders before calling AWS. Modules/build targets are checked against the statically linked application; build targets need not be duplicated in JSON.

Authenticate normally, for example with `aws sso login --profile YOUR_PROFILE` and `AWS_PROFILE`. Initialize the application-owned stack:

```sh
pulumi login s3://YOUR_ATTESTRA_STATE_BUCKET
cd infra
pulumi stack init dev
pulumi config set aws:region us-east-1
pulumi config set --path 'aws:allowedAccountIds[0]' YOUR_12_DIGIT_ACCOUNT
pulumi config set appOrigin https://YOUR_APP_HOST
pulumi config set adminOrigin https://YOUR_ADMIN_HOST
pulumi config set senderDomain YOUR_SENDER_DOMAIN
pulumi config set senderAddress hello@YOUR_SENDER_DOMAIN
pulumi config set adminCertificateArn YOUR_REGIONAL_ACM_CERTIFICATE_ARN
pulumi config set --secret proofKey
```

For `proofKey`, enter base64 encoding of at least 32 cryptographically random bytes at the prompt. Do not reuse an example value or put it in a manifest. Keep the Pulumi passphrase outside Git. `infra/Pulumi.example.yaml` documents the nonsecret shape; actual stack files are ignored.

### Bootstrap and verify SES before full deployment

Cognito requires the SES sender to be verified before configuring developer email ([AWS documentation](https://docs.aws.amazon.com/cognito/latest/developerguide/user-pool-email.html)). For a **new** stack, first set `senderOnly` to true in `infra`:

```sh
pulumi config set senderOnly true
```

Return to the repository root and run `go run ./cmd/attestra deploy --env dev`. This stage creates only the SES identity/DKIM resources under the same parent/names as the pinned auth component. Publish the TXT/DKIM records from the outputs and wait until the identity is verified. Confirm its status using the AWS SES console or the read-only command:

```sh
aws ses get-identity-verification-attributes --identities YOUR_SENDER_DOMAIN --region YOUR_REGION
```

After verification succeeds, set `senderOnly` to false in `infra`. Full provisioning includes a Pulumi SES verification waiter and makes authentication depend on it. The sender resources retain their exact logical identity. **Never turn senderOnly back on in an already fully deployed stack**: that would propose removing the other resources; protections help but do not replace this rule. Use only the normal full mode for subsequent updates.

Return to the repository root:

```sh
go run ./cmd/attestra check --env dev
go run ./cmd/attestra preview --env dev
# After reviewing the preview:
go run ./cmd/attestra deploy --env dev
go run ./cmd/attestra status --env dev
```

The library's engine verifies AWS identity, selected stack, region and allowed-account restriction, builds archives, and invokes Pulumi. Runtime and infrastructure use the same route declarations. `preview` does not run `up`.

## DNS, email and admin onboarding

After an authorized deployment, read outputs:

- `apiUrl`, `healthUrl`: application API and public process health.
- `adminUrl`, `adminDnsTarget`: publish the admin hostname as a DNS record pointing to the API Gateway target. The certificate and mapping are provisioned; DNS is operator-owned.
- `sesVerificationToken`, `sesDkimTokens`: retain the SES verification/DKIM records published during bootstrap. SES sandbox restrictions still apply; request production sending access before real-user delivery.
- `userPoolId`, `clientId`, `issuer`: customer integration configuration.
- `adminPoolId`, `adminClientId`, `configTableName`: administrator identity/grant setup.

The app origin must host the confirmation/deep-link client at `/verify-email`; this backend does not implement that client UI. Use the existing authentication client against the exported API/pool/client values.

Invite an administrator through Cognito using an operator role and complete password/TOTP enrollment. Create an explicit grant row in the config table:

| DynamoDB attribute | Type | Value |
| --- | --- | --- |
| `pk` | String | `GRANT#dev` |
| `sk` | String | Administrator Cognito subject |
| `active` | Boolean | `true` |
| `permissions` | String set | `admin:access`, `attestra:admin` |

No automatic administrator is created. The admin runtime cannot write grant rows. Sign in at `adminUrl` through the dedicated managed-login pool. The initial Attestra module contributes a page and status endpoint; it does not register a module configuration schema. Additional configuration panels can be added through the library contract later.

The library's additional legacy administrator passkey-MFA reconciliation is not automatically enabled here; the new admin component supplies the framework's baseline MFA/PKCE behavior. Customer passkey APIs are wired to the Cognito regional endpoint.

## GitHub Actions

Verification runs on push and PR without AWS credentials. The deployment workflow is **manual only**, defaults to preview, uses an explicitly selected GitHub environment and OIDC, and does not offer teardown.

Create a protected GitHub environment with:

- Variables: `DEPLOY_ROLE_ARN`, `AWS_REGION`, `AWS_ACCOUNT_ID`, `APPLICATION_MANIFEST` (the full nonsecret manifest JSON).
- Secrets: `PULUMI_CONFIG_PASSPHRASE`, `PULUMI_STACK_YAML` (the existing selected stack YAML, including encrypted Pulumi secrets).

Restrict the AWS OIDC trust policy to this repository and the specific GitHub environment. Keep region/account values consistent with the manifest and stack. These remote settings and roles were not created by this setup.

## Destruction

Application identity pools and tables are protected. The CLI forwards the library's explicit destruction guards and does not automatically remove protection or delete data outside the stack:

```sh
go run ./cmd/attestra teardown --env prod --allow-destroy --allow-production \
  --confirm 'attestra/prod/ACCOUNT/REGION/prod/s3://STATE_BUCKET'
```

This is documentation, not an instruction executed during setup. Review retention/exports, exact state ownership and resource protection separately. New application resources are not enrolled in the old environment-reset inventory.

## Pinned-library browser compatibility

The application wraps the admin shell response to add `connect-src` for its exact configured HTTPS Cognito login origin. The pinned library otherwise inherits a self-only CSP that blocks the browser OAuth token exchange. This narrow adapter is tested and can be removed after updating to a library revision that supplies the correct CSP. It does not allow arbitrary external origins.

Browser-session extension compatibility: the adapter also forwards the host-only session cookie as a bearer header for `/admin/api/extensions/` requests because the pinned shell does not forward it internally. The normal token/grant checks still execute, and unsafe cookie methods must pass the same-origin check before adaptation.
