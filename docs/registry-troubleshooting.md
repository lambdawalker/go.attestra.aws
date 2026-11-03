# Registry update failures during deployment

A registry change may run before Lambda builds or Pulumi updates. A failure there stops the deployment; `-timed-confirm` does not bypass registry errors, change signing, or grant permissions.

The old client suggested deployment-role `execute-api` permissions for every failure. That advice was too broad. HTTP401/403 calls for checking credentials/signing/caller permissions. HTTP5xx calls for inspecting the shared index service or API Gateway integration; a DynamoDB failure inside the index Lambda can also produce503. An environment lock/superseded receipt remains a409 and keeps its existing recovery flow.

With this diagnostic update, the client prints the operation, environment and API request ID when available. The shared index Lambda logs `registry.change_failed` with that request ID, backend error code and redacted details. Backend details stay out of the public response; the fencing token is not logged. Retries keep the exact original token.

## Install diagnostics without deploying the app

After merging/pulling the update, refresh the shared index/admin deployment:

```powershell
.\deploy.bat -scope shared -timed-confirm
```

This is a real shared deployment: review its preview. It does not redeploy the environment backend. Then retry the original app deployment. A persistent503 should now include a request ID that can be matched to the new error log.

Discover the shared index Lambda name with read-only AWS CLI commands:

```powershell
aws lambda list-functions --profile attestra --region us-east-2 --query "Functions[?starts_with(FunctionName, 'index-handler-')].FunctionName" --output text
aws logs tail "/aws/lambda/<index-handler-name>" --profile attestra --region us-east-2 --since 15m
```

Use the function for this project's shared index if more than one is returned. Locate `registry.change_failed` and match `request_id` to the client error. An AWS error such as `AccessDeniedException`, `ResourceNotFoundException`, or `ValidationException` identifies the next investigation; a503 alone does not establish which one occurred. Logs from before this diagnostic update may contain only normal invocation reports because the handler previously discarded the underlying error.

Retain pending deployment receipts while investigating. This update does not delete state, clear locks, change IAM grants, or retry with a new fencing token. If the tool subsequently reports a pending/interrupted registry operation, use the existing explicit recovery workflow after checking its recorded state.

## Defaults synchronization and reserved attribute names

A `defaults` update of an existing runtime configuration previously used `Override` directly in its DynamoDB condition expression. `OVERRIDE` is a reserved keyword (case-insensitive), so DynamoDB can reject that transaction with a validation error and the registry returns503. The auth runtime store now aliases this name as `#override`; the stored attribute and administrator override protection are unchanged. No data migration is required.

After pulling the application dependency update, deploy the shared scope first using the command above, then retry the original deployment. Updating the local CLI alone does not replace the runtime store compiled into the deployed shared index Lambda. Keep any pending receipt so the existing recovery flow can resume it. If503 persists, correlate the new request ID with `registry.change_failed` rather than assuming the same cause.
