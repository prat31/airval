#!/bin/sh
# Lint and render the chart with every CI values file, then validate the manifests with kubeconform.
# Uses local helm/kubeconform when installed, otherwise Docker.
#   deploy/helm/validate.sh
set -eu
cd "$(dirname "$0")/../.."
CHART=deploy/helm/airval
HELM_IMAGE=${HELM_IMAGE:-alpine/helm:3.16.4}
KUBECONFORM_IMAGE=${KUBECONFORM_IMAGE:-ghcr.io/yannh/kubeconform:v0.6.7}
K8S_VERSION=${K8S_VERSION:-1.31.0}
OUT=${OUT:-$(mktemp -d)}
mkdir -p "$OUT"

run_helm() {
  if command -v helm >/dev/null 2>&1; then command helm "$@"
  else docker run --rm -v "$PWD:/w" -w /w "$HELM_IMAGE" "$@"; fi
}
run_kubeconform() {
  if command -v kubeconform >/dev/null 2>&1; then command kubeconform "$@"
  else docker run --rm -i -v "$OUT:/out" "$KUBECONFORM_IMAGE" "$@"; fi
}

run_helm dependency build "$CHART"
for v in "$CHART"/ci/*-values.yaml; do
  name=$(basename "$v" .yaml)
  echo "== $name"
  run_helm lint --strict "$CHART" -f "$v"
  run_helm template airval "$CHART" -f "$v" --namespace airval > "$OUT/$name.yaml"
done

# Invalid combinations must be rejected.
if run_helm template airval "$CHART" >/dev/null 2>&1; then echo "expected failure without a database"; exit 1; fi
echo "== kubeconform"
for f in "$OUT"/*-values.yaml; do
  run_kubeconform -strict -summary -ignore-missing-schemas \
    -kubernetes-version "$K8S_VERSION" \
    -schema-location default \
    -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
    "/out/$(basename "$f")"
done
echo "OK"
