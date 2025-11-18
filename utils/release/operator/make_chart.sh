#!/usr/bin/env bash

mod_version() {
  version=$1
  if [ "$version" == "0.0.0" ]; then
    echo "latest"
  else
    echo "v$version"
  fi
}

# Set desired Wasm-shim image
wasm_shim_version=$(mod_version $(yq '.dependencies.wasm-shim' $env/release.yaml))
wasm_shim_image="oci://quay.io/kuadrant/wasm-shim:$wasm_shim_version"
V=$wasm_shim_image \
yq eval '(select(.kind == "Deployment").spec.template.spec.containers[].env[] | select(.name == "RELATED_IMAGE_WASMSHIM").value) = strenv(V)' --inplace $env/config/manager/manager.yaml

# Set desired developer-portal-controller image
developerportal_version=$(mod_version $(yq '.dependencies.developer-portal-controller' $env/release.yaml))
developerportal_image="quay.io/kuadrant/developer-portal-controller:$developerportal_version"
V=$developerportal_image \
yq eval '(select(.kind == "Deployment").spec.template.spec.containers[].env[] | select(.name == "RELATED_IMAGE_DEVELOPERPORTAL").value) = strenv(V)' --inplace $env/config/manager/manager.yaml

# Set desired operator image
cd $env/config/manager
operator_version=$(mod_version $(yq '.kuadrant-operator.version' $env/release.yaml))
operator_image=quay.io/kuadrant/kuadrant-operator:$operator_version
kustomize edit set image controller=$operator_image
cd -

# Build the helm chart templates from kustomize manifests
kustomize build $env/config/helm > $env/charts/kuadrant-operator/templates/manifests.yaml

# Set the helm chart version and dependencies versions
operator_version=$(mod_version $(yq '.kuadrant-operator.version' $env/release.yaml))
V="$(yq '.kuadrant-operator.version' $env/release.yaml)" yq --inplace eval '.version = strenv(V)' $env/charts/kuadrant-operator/Chart.yaml
V="$(yq '.kuadrant-operator.version' $env/release.yaml)" yq --inplace eval '.appVersion = strenv(V)' $env/charts/kuadrant-operator/Chart.yaml
V="$(yq '.dependencies.authorino-operator' $env/release.yaml)" yq --inplace eval '(.dependencies[] | select(.name == "authorino-operator").version) = strenv(V)' $env/charts/kuadrant-operator/Chart.yaml
V="$(yq '.dependencies.limitador-operator' $env/release.yaml)" yq --inplace eval '(.dependencies[] | select(.name == "limitador-operator").version) = strenv(V)' $env/charts/kuadrant-operator/Chart.yaml
V="$(yq '.dependencies.dns-operator' $env/release.yaml)" yq --inplace eval '(.dependencies[] | select(.name == "dns-operator").version) = strenv(V)' $env/charts/kuadrant-operator/Chart.yaml
