#!/usr/bin/env bash
set -euo pipefail
CHART=charts/__HIROBA_NAME__
CURRENT=$(helm show chart "$HIROBA_STACK_ROOT/$CHART" | awk '/^version:/ {print $2}')
VERSION=$CURRENT
if [[ "$HIROBA_STACK_PHASE" == install ]]; then VERSION="$CURRENT-ci.0"; fi
OUT="$HIROBA_STACK_WORK/$HIROBA_STACK_PHASE"
mkdir -p "$OUT"
python3 "$HIROBA_WORKFLOW_ROOT/scripts/prepare-chart.py" --chart "$HIROBA_STACK_ROOT/$CHART"
helm package "$HIROBA_STACK_ROOT/$CHART" --version "$VERSION" --destination "$OUT" >/dev/null
PACKAGE="$OUT/__HIROBA_NAME__-$VERSION.tgz"
DIGEST=$(sha256sum "$PACKAGE" | awk '{print $1}')
python3 "$HIROBA_WORKFLOW_ROOT/scripts/chart-test-config.py" --root "$HIROBA_STACK_ROOT" --chart "$CHART" > "$OUT/chart-test.json"
if [[ $(jq -r '.image != null' "$OUT/chart-test.json") == true ]]; then
  repository=$(jq -r .image.repository "$OUT/chart-test.json")
  tag=$(jq -r .image.tag "$OUT/chart-test.json")
  context=$(jq -r .image.context "$OUT/chart-test.json")
  docker build -t "$repository:$tag" "$HIROBA_STACK_ROOT/$context"
  kind load docker-image "$repository:$tag" --name "$HIROBA_STACK_CLUSTER"
fi
while read -r dependency; do
  [[ -n "$dependency" ]] || continue
  kubectl apply -f "$HIROBA_STACK_ROOT/$dependency"
done < <(jq -r '.dependencies[]' "$OUT/chart-test.json")
helm upgrade --install __HIROBA_HELM_RELEASE__ "$PACKAGE" --namespace default \
  --values "$HIROBA_STACK_ROOT/$CHART/ci/kind-values.yaml" --wait --timeout 5m
__HIROBA_STACK_PACK_TEST__jq -n --arg phase "$HIROBA_STACK_PHASE" --arg version "$VERSION" --arg digest "sha256:$DIGEST" \
  '{schema:"inari.stack-phase-result.v1",phase:$phase,version:$version,contentDigest:$digest,converged:true}' \
  > "$HIROBA_STACK_WORK/$HIROBA_STACK_PHASE-result.json"
