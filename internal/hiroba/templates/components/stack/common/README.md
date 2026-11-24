# Stack lifecycle testing

This classified stack always runs an initial install followed by an upgrade in the same fresh kind cluster. The shared workflow owns central tool/setup pins, exact-content caching, diagnostics, cleanup, and source-bound evidence. These phases prove wiring and convergence only, not functional upstream E2E or deployed production readiness.

Keep install.sh and upgrade.sh executable and record distinct exact content identities in their phase results. Do not create another cluster in either phase.
