#!/usr/bin/env bash
set -euo pipefail
VERSION=0.1.0
if [[ "$HIROBA_STACK_PHASE" == install ]]; then VERSION=0.1.0-ci.0; fi
RENDERED="$HIROBA_STACK_WORK/$HIROBA_STACK_PHASE-fixture.yaml"
python3 - "$HIROBA_STACK_ROOT/tests/stack/fixture.yaml" "$RENDERED" "$HIROBA_STACK_PHASE" <<'PY'
import sys, yaml
source, output, phase = sys.argv[1:]
data = yaml.safe_load(open(source))
data["spec"]["template"]["metadata"].setdefault("annotations", {})["inari.dev/ci-revision"] = phase
with open(output, "w") as target:
    yaml.safe_dump(data, target, sort_keys=False)
PY
kubectl apply -f "$RENDERED"
kubectl rollout status deployment/__HIROBA_FIXTURE_NAME__ -n default --timeout=120s
test "$(kubectl get deployment __HIROBA_FIXTURE_NAME__ -n default -o jsonpath='{.spec.template.metadata.annotations.inari\.dev/ci-revision}')" = "$HIROBA_STACK_PHASE"
DIGEST=$(sha256sum "$RENDERED" | awk '{print $1}')
jq -n --arg phase "$HIROBA_STACK_PHASE" --arg version "$VERSION" --arg digest "sha256:$DIGEST" \
  '{schema:"inari.stack-phase-result.v1",phase:$phase,version:$version,contentDigest:$digest,converged:true}' \
  > "$HIROBA_STACK_WORK/$HIROBA_STACK_PHASE-result.json"
