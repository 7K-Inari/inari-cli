# inari-cli — Agent Guide

The `inari` CLI: login (OIDC device flow), cluster/catalog/resource ops, agent install, extension scaffolding (`inari extension init`) (plan §6 #9).

Stack: Go, cobra

## Key architecture constraints
- v1 scope is **core flows only**: login, cluster register, catalog, deploy, resources — full console parity is explicitly NOT a v1 goal (§11/4).
- Auth: OIDC device flow against the platform Keycloak `inari` realm; tokens carry org/team claims (§5.4).
- Consume REST/OpenAPI contracts from the `inari-api` repo (generated Go client); pin versions (§6).
- Binary releases via brew/scoop/go install (goreleaser) (§6).

## Conventions
- Conventional Commits; SemVer releases; container images/artifacts cosign-signed (once CI exists).
- Write tests for new behavior; keep changes minimal and focused.
- Canonical architecture & development plan: https://github.com/7K-Inari/inari-docs/blob/main/docs/architecture/inari-platform-plan.md (section references below point into it).

## Platform design principles (apply everywhere)
1. Tenant-aware to the core — every object carries a tenant ID; every API decision is tenant-scoped.
2. Zero tenant credentials on the hub — no tenant kubeconfigs or cloud keys in the control plane.
3. Pull, never push — agents dial out; the control plane never initiates connections into tenant networks.
4. Desired state, eventually reconciled — GitOps/CR-based mutations, not imperative RPCs.
5. The catalog is a projection of reality — capabilities are discovered, not declared.
6. Small kernel, everything else extension.
7. Modular monolith first — strict internal module boundaries.
