#!/usr/bin/env bash
set -euo pipefail
kubectl get all,configmaps,prometheusrules -A -o wide || true
