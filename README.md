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
inari cluster register prod-eu --label env=prod
# Then run the printed Helm command to install inari-agent with its one-time token.
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

Extension scaffolds include `extension.yaml` contract declarations (`inari.plugin.v1` or `inari.ui.v1`). When a server is configured, the command makes a bounded, unauthenticated `GET /api/v1/meta/contracts` check and warns if the generated contract is not advertised. Offline, old-server, auth, network, and malformed-response failures never block scaffolding; no tenant or interactive login is required.

Every command supports `-o table|json|yaml` and a genuinely helpful `--help`.

## Hiroba community scaffolding

```sh
inari hiroba init my-service
inari hiroba init my-service --type app --with-plugin --with-ui --with-chart --with-observability --with-runtime-smoke
inari hiroba init my-catalog --type catalog --with-observability
inari hiroba init my-template --type template --with-runtime-smoke
```

`--type` selects `app`, `extension`, `catalog`, or `template`; `--dir` chooses the parent and `--module` overrides the Go module. Every boolean component combination is supported. Observability always includes canonical assets, full signed package metadata, and the v2 companion pack, with chart-native output only when `--with-chart` is selected. Component templates can carry a confined `observability.manifest` declaration. Runtime smoke uses the actual chart through its generic image declaration or, without a chart, a meaningful CI-only fixture under `tests/smoke` without adding an application component. Long registry IDs receive deterministic hash-suffixed Helm/resource names where needed, without changing the permanent app or publication identity.

Generated workflow callers use full signed-release commit SHAs and version comments, never mutable branches or major-version tags. Package examples use canonical `<source>/<package>` identities. New scaffolds declare kind-qualified `releaseComponents` so same-named packages, templates, charts, and standalone smoke suites have unique signed Release tags; backend and UI compatibility evidence and publication also work independently. The app ID `observability-pack` is reserved for the canonical companion package. Fixture-only suites are machine-marked as CI-only and ineligible for reviewed/official application evidence or real stage/prod verification.

When a backend or UI component is generated and a server is configured, `inari hiroba init` makes a bounded, unauthenticated `GET /api/v1/meta/contracts` request. Incompatible or unavailable contracts produce warnings only; offline generation and no-server behavior remain nonblocking.

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
