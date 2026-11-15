# Fixture-only runtime smoke

This chartless suite deploys the digest-pinned synthetic HTTP application in `fixtures/app/` and asserts its Service response through Argo Workflows. It uses the explicit `manifests` adapter and contains no application chart, package, or plugin component.

`fixtureOnly: true`, `executableProfiles: [ci]`, `evidenceScope: fixture`, and `appEvidenceEligible: false` are machine-enforced. This suite proves only the synthetic fixture. It cannot satisfy reviewed/official application runtime evidence or real stage/prod application verification. Replace it with an artifact-bound target before making those claims.

Run the local CI journey with the workflow library's `test-runtime-smoke.yml` caller or `run-runtime-smoke.sh --mode local --profile ci`.
