#!/usr/bin/env bash

echo "make bundle"
root=$(pwd)
cd $env
operator-sdk generate kustomize manifests --interactive=false

# Set desired Wasm-shim image
wasm_shim=$(yq '.dependencies.Wasm_shim' $env/release.toml)
V="oci://quay.io/kuadrant/wasm-shim:v$wasm_shim" \
yq eval '(select(.kind == "Deployment").spec.template.spec.containers[].env[] | select(.name == "RELATED_IMAGE_WASMSHIM").value) = strenv(V)' --inplace config/manager/manager.yaml

# Set desired ConsolePlugin image
console_plugin=$(yq '.dependencies.Console_plugin' $env/release.toml)
V="quay.io/kuadrant/console-plugin:v$console_plugin" \
yq eval '(select(.kind == "Deployment").spec.template.spec.containers[].env[] | select(.name == "RELATED_IMAGE_CONSOLEPLUGIN").value) = strenv(V)' --inplace config/manager/manager.yaml

# Set desired operator image
cd $env/config/manager
# FIX: for the minute the values for the org and registry are hardcoded into the operator image.
# This should not be the case.

operator_version=$(yq '.kuadrant.release' $env/release.toml)
operator_image=quay.io/kuadrant/kuadrant-operator:v$operator_version
kustomize edit set image controller=$operator_image

csv=$env/config/manifests/bases/kuadrant-operator.clusterserviceversion.yaml
V="kuadrant-operator.v$operator_version" yq eval '.metadata.name = strenv(V)' --inplace $csv
V="$operator_version" yq eval '.spec.version = strenv(V)' --inplace $csv
V="$operator_image" yq eval '.metadata.annotations.containerImage = strenv(V)' --inplace $csv

cd -

channels=$(yq -oy '.kuadrant.channels | join(",")' $env/release.toml)
default_channel=$(yq '.kuadrant.default_channel' $env/release.toml)
kustomize build config/manifests | operator-sdk generate bundle -q --overwrite --version $operator_version --channels $channels --default-channel $default_channel

openshift_version_annotation_key="com.redhat.openshift.versions"
# Supports Openshift v4.12+ (https://redhat-connect.gitbook.io/certified-operator-guide/ocp-deployment/operator-metadata/bundle-directory/managing-openshift-versions)
openshift_supported_versions="v4.12"
key=$openshift_version_annotation_key value=$openshift_supported_versions yq --inplace '.annotations[strenv(key)] = strenv(value)' bundle/metadata/annotations.yaml
key=$openshift_version_annotation_key yq --inplace '(.annotations[strenv(key)] | key) headComment = "Custom annotations"' bundle/metadata/annotations.yaml

echo "reading data form quay.io, slow process."
dep_file="$env/bundle/metadata/dependencies.yaml"

limitador_version=$(yq '.dependencies.Limitador' $env/release.toml)
limitador_image=quay.io/kuadrant/limitador-operator-bundle:v$limitador_version
V=$(opm render $limitador_image | yq eval '.properties[] | select(.type == "olm.package") | .value.version' -)

COMPONENT=limitador-operator V=$V \
  yq eval '(.dependencies[] | select(.value.packageName == strenv(COMPONENT)).value.version) = strenv(V)' -i $dep_file


authorino_version=$(yq '.dependencies.Authorino' $env/release.toml)
authorino_image=quay.io/kuadrant/authorino-operator-bundle:v$authorino_version
V=$(opm render $authorino_image | yq eval '.properties[] | select(.type == "olm.package") | .value.version' -)

COMPONENT=authorino-operator V=$V \
  yq eval '(.dependencies[] | select(.value.packageName == strenv(COMPONENT)).value.version) = strenv(V)' -i $dep_file


dns_version=$(yq '.dependencies.DNS' $env/release.toml)
dns_image=quay.io/kuadrant/dns-operator-bundle:v$dns_version
V=$(opm render $dns_image | yq eval '.properties[] | select(.type == "olm.package") | .value.version' -)

COMPONENT=dns-operator V=$V \
  yq eval '(.dependencies[] | select(.value.packageName == strenv(COMPONENT)).value.version) = strenv(V)' -i $dep_file
echo "finished reading data form quay.io, slow."

operator-sdk bundle validate $env/bundle
git diff --quiet -I'^    createdAt: ' ./bundle && git checkout ./bundle || true

quay_expiry_time_label="
# Quay image expiry
ARG QUAY_IMAGE_EXPIRY
ENV QUAY_IMAGE_EXPIRY=\${QUAY_IMAGE_EXPIRY:-never}
LABEL quay.expires-after=\${QUAY_IMAGE_EXPIRY}
"
echo -en "$quay_expiry_time_label" >> bundle.Dockerfile

# exit script and return to initail directory
cd $root
