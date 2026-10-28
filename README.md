# Attestra on Apexfission

Attestra owns its deployments and composes reusable authentication, backend,
administrator and environment-index packages from
[go.apexfission.aws.auth](https://github.com/lambdawalker/go.apexfission.aws.auth).

Run the Go deployment wizard from this checkout:

```sh
./deploy.sh
```

On Windows:

```bat
deploy.bat
```

The wizard supports shared admin/index, a selected backend environment, or both.
Its initial domain prefix is `next`. Explicitly choosing an empty prefix selects
a separate final deployment; it does not rename the test deployment.

| Scope | Pulumi project | Prefixed stack | Unprefixed stack |
| --- | --- | --- | --- |
| Shared administrator/index | attestra-shared | next-shared | final-shared |
| Backend dev | attestra-backend | next-dev | final-dev |

The new projects do not adopt the legacy `attestra-auth-email` or
`attestra-index` projects. Both deployments may coexist while you test.

The backend retains the original email confirmation, sign-in/password/passkey,
runtime configuration, ID capture and environment reset contracts. The shared
stack supplies the original environment index and administrator console with
invitation-only MFA. The separate generic application module under `app/`
remains an example of extending the newer framework; the wizard deploys the
complete compatibility backend.

See [deployment](docs/deployment.md) for prerequisites, first deployment,
Android configuration, shared email ownership and the final cutover.

## Local verification

Requires Go 1.26.6:

```sh
go test -race ./...
go vet ./...
go -C infra test ./...
go -C infra-index test ./...
go run ./cmd/attestra build
```

The build command creates all 22 Linux ARM64 Lambda ZIPs under `dist/` without
AWS access. `cmd/attestra` retains local demonstration manifest operations;
cloud operations use `cmd/deploy` and the launchers.

No live AWS deployment or teardown is part of local verification.
