#!/usr/bin/env bash

# Set desired Wasm-shim image
wasm_shim_version=$(yq '.dependencies.Wasm_shim' $env/release.toml)
wasm_shim_image="oci://quay.io/kuadrant/wasm-shim:v$wasm_shim_version"
V=$wasm_shim_image \
yq eval '(select(.kind == "Deployment").spec.template.spec.containers[].env[] | select(.name == "RELATED_IMAGE_WASMSHIM").value) = strenv(V)' --inplace $env/config/manager/manager.yaml

# Set desired ConsolePlugin image
consoleplugin_version=$(yq '.dependencies.Console_plugin' $env/release.toml)
consoleplugin_image="quay.io/kuadrant/console-plugin:v$consoleplugin_version"
V=$consoleplugin_image \
yq eval '(select(.kind == "Deployment").spec.template.spec.containers[].env[] | select(.name == "RELATED_IMAGE_CONSOLEPLUGIN").value) = strenv(V)' --inplace $env/config/manager/manager.yaml

# Set desired operator image
cd $env/config/manager
operator_version=$(yq '.kuadrant.release' $env/release.toml)
operator_image=quay.io/kuadrant/kuadrant-operator:v$operator_version
kustomize edit set image controller=$operator_image
cd -

# Build the helm chart templates from kustomize manifests
kustomize build $env/config/helm > $env/charts/kuadrant-operator/templates/manifests.yaml

# Set the helm chart version and dependencies versions
operator_version=$(yq '.kuadrant.release' $env/release.toml)
V="$(yq '.kuadrant.release' $env/release.toml)" yq --inplace eval '.version = strenv(V)' $env/charts/kuadrant-operator/Chart.yaml
V="$(yq '.kuadrant.release' $env/release.toml)" yq --inplace eval '.appVersion = strenv(V)' $env/charts/kuadrant-operator/Chart.yaml
V="$(yq '.dependencies.Authorino_bundle' $env/release.toml)" yq --inplace eval '(.dependencies[] | select(.name == "authorino-operator").version) = strenv(V)' $env/charts/kuadrant-operator/Chart.yaml
V="$(yq '.dependencies.Limitador_bundle' $env/release.toml)" yq --inplace eval '(.dependencies[] | select(.name == "limitador-operator").version) = strenv(V)' $env/charts/kuadrant-operator/Chart.yaml
V="$(yq '.dependencies.DNS_bundle' $env/release.toml)" yq --inplace eval '(.dependencies[] | select(.name == "dns-operator").version) = strenv(V)' $env/charts/kuadrant-operator/Chart.yaml
