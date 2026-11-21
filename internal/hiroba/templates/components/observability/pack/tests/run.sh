#!/usr/bin/env bash
set -euo pipefail
PACKAGE_DIR=$(cd "$(dirname "$0")/.." && pwd)
kubectl apply -f "$PACKAGE_DIR/tests/deps/prometheusrule-crd.yaml"
kubectl apply -f "$PACKAGE_DIR/rgd.yaml"
kubectl wait --for=condition=Ready rgd/__HIROBA_NAME__-observability --timeout=120s
kubectl apply -f "$PACKAGE_DIR/tests/fixture.yaml"
for _ in $(seq 1 60); do
  if kubectl get configmap __HIROBA_NAME__-overview -n default >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
kubectl get configmap __HIROBA_NAME__-overview -n default
kubectl get configmap __HIROBA_NAME__-folder -n default
kubectl get prometheusrule __HIROBA_NAME__-central -n default
