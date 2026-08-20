# inari-cli

The `inari` CLI: login (OIDC device flow), cluster/catalog/resource ops, agent install, extension scaffolding (`inari extension init`) (plan §6 #9).

Stack: Go, cobra

Part of the **Inari** multi-tenant Internal Developer Platform (GitHub org `7K-Inari`).
Canonical architecture & development plan: [inari-docs/docs/architecture/inari-platform-plan.md](https://github.com/7K-Inari/inari-docs/blob/main/docs/architecture/inari-platform-plan.md)

## Install

```sh
go install github.com/7K-Inari/inari-cli@latest
# or via Homebrew / Scoop once a release is published:
brew install 7K-Inari/tap/inari
scoop bucket add inari https://github.com/7K-Inari/scoop-bucket && scoop install inari
```

## Core flows

```sh
# Authenticate (OIDC device flow against the platform Keycloak 'inari' realm)
inari login                                   # prints user code + verification URL

# Manage contexts (server/issuer/tenant; ~/.config/inari/config.yaml)
inari context list
inari context use prod
inari context set prod --server https://api.inari.example.com --tenant acme

# Register a cluster and install the agent (manifest embeds a one-time token)
inari cluster register prod-eu --label env=prod | kubectl apply -f -
inari cluster list

# Browse the catalog
inari catalog list --cluster clu-1
inari catalog describe postgres-aws -o yaml

# Deploy (interactive wizard, or non-interactive for CI)
inari deploy postgres-aws --cluster clu-1
inari deploy postgres-aws --cluster clu-1 --file values.yaml --set replicas=3
inari deploy postgres-aws --cluster clu-1 --file values.yaml --dry-run   # policy pre-flight

# Cross-cluster inventory
inari resources list --health Degraded
inari resources get inst-1

# Scaffold an extension
inari extension init my-ext --type backend
inari extension init my-ui-ext --type ui
```

Every command supports `-o table|json|yaml` and a genuinely helpful `--help`.

## Configuration

- `~/.config/inari/config.yaml` — kubectl-style contexts (`server`, `issuer`, `tenant`).
- `~/.config/inari/tokens/<context>.json` — cached OIDC tokens, mode 0600, refreshed automatically.
- Env overrides: `INARI_SERVER`, `INARI_ISSUER`, `INARI_CONFIG_DIR`.

## Development

```sh
go test ./...
go vet ./...
goreleaser check
```

Releases are driven by release-please (PR-only mode): merging the Release PR triggers goreleaser (brew/scoop/go-install binaries). Use Conventional Commits.

API types come from the pinned generated client in [`inari-api`](https://github.com/7K-Inari/inari-api) (`gen/go/oas`) — never hand-rolled. Bump the pin in `go.mod` when inari-api releases.
