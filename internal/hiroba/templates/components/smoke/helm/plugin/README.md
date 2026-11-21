# Runtime smoke

This artifact-bound suite deploys the actual `charts/__HIROBA_NAME__` chart through the explicit `helm` adapter, builds the generic chart-test image declaration, and asserts the extension health and ping endpoints through Argo Workflows.

Only the local `ci` profile executes in this repository. Stage/prod declarations require the separately gated environment verifier and exact approved application/suite digests.
