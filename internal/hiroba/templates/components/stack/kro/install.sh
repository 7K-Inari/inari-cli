#!/usr/bin/env bash
set -euo pipefail
PACKAGE="$HIROBA_STACK_ROOT/packages/observability-pack"
VERSION=0.1.0
if [[ "$HIROBA_STACK_PHASE" == install ]]; then VERSION=0.1.0-ci.0; fi
RENDERED="$HIROBA_STACK_WORK/$HIROBA_STACK_PHASE-rgd.yaml"
python3 - "$PACKAGE/rgd.yaml" "$RENDERED" "$HIROBA_STACK_PHASE" <<'PY'
import sys, yaml
source, output, phase = sys.argv[1:]
data = yaml.safe_load(open(source))
data["metadata"].setdefault("annotations", {})["inari.dev/ci-revision"] = phase
with open(output, "w") as target:
    yaml.safe_dump(data, target, sort_keys=False)
PY
kubectl apply -f "$PACKAGE/tests/deps/prometheusrule-crd.yaml"
kubectl apply -f "$RENDERED"
kubectl wait --for=condition=Ready rgd/__HIROBA_NAME__-observability --timeout=120s
kubectl apply -f "$PACKAGE/tests/fixture.yaml"
for _ in $(seq 1 60); do
  if kubectl get configmap __HIROBA_NAME__-overview -n default >/dev/null 2>&1; then break; fi
  sleep 2
done
kubectl get configmap __HIROBA_NAME__-overview -n default >/dev/null
kubectl get configmap __HIROBA_NAME__-folder -n default >/dev/null
kubectl get prometheusrule __HIROBA_NAME__-central -n default >/dev/null
DIGEST=$(sha256sum "$RENDERED" | awk '{print $1}')
jq -n --arg phase "$HIROBA_STACK_PHASE" --arg version "$VERSION" --arg digest "sha256:$DIGEST" \
  '{schema:"inari.stack-phase-result.v1",phase:$phase,version:$version,contentDigest:$digest,converged:true}' \
  > "$HIROBA_STACK_WORK/$HIROBA_STACK_PHASE-result.json"
