# Runtime smoke

This artifact-bound suite deploys the actual `charts/__HIROBA_NAME__` chart through the explicit `helm` adapter and asserts the non-plugin HTTP workload through Argo Workflows. No extension component or extension Docker build is required.

Only the local `ci` profile executes in this repository. Stage/prod declarations require the separately gated environment verifier and exact approved application/suite digests.
