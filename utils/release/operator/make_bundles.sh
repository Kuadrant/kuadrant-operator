#!/usr/bin/env bash

mod_version() {
  version=$1
  if [ "$version" == "0.0.0" ]; then
    echo "latest"
  else
    echo "v$version"
  fi
}

echo "make bundle"
root=$(pwd)
cd $env
operator-sdk generate kustomize manifests --interactive=false

# Set desired Wasm-shim image
wasm_shim=$(mod_version $(yq '.dependencies.wasm-shim' $env/release.yaml))
V="oci://quay.io/kuadrant/wasm-shim:$wasm_shim" \
yq eval '(select(.kind == "Deployment").spec.template.spec.containers[].env[] | select(.name == "RELATED_IMAGE_WASMSHIM").value) = strenv(V)' --inplace config/manager/manager.yaml

# Set desired ConsolePlugin image
console_plugin=$(mod_version $(yq '.dependencies.console-plugin' $env/release.yaml))
V="quay.io/kuadrant/console-plugin:$console_plugin" \
yq eval '(select(.kind == "Deployment").spec.template.spec.containers[].env[] | select(.name == "RELATED_IMAGE_CONSOLEPLUGIN").value) = strenv(V)' --inplace config/manager/manager.yaml

# Set desired operator image
cd $env/config/manager
# FIX: for the minute the values for the org and registry are hardcoded into the operator image.
# This should not be the case.

operator_version=$(yq '.kuadrant-operator.version' $env/release.yaml)
operator_image=quay.io/kuadrant/kuadrant-operator:$(mod_version $operator_version)
kustomize edit set image controller=$operator_image

csv=$env/config/manifests/bases/kuadrant-operator.clusterserviceversion.yaml
V="kuadrant-operator.v$operator_version" yq eval '.metadata.name = strenv(V)' --inplace $csv
V="$operator_version" yq eval '.spec.version = strenv(V)' --inplace $csv
V="$operator_image" yq eval '.metadata.annotations.containerImage = strenv(V)' --inplace $csv

cd -

channels=$(yq '.olm.channels | join(",")' $env/release.yaml)
default_channel=$(yq '.olm.default-channel' $env/release.yaml)
default_channel_opt="--default-channel $default_channel"
if [[ "$default_channel" == "null" ]]; then
  default_channel_opt=""
fi

kustomize build config/manifests | operator-sdk generate bundle -q --overwrite --version $operator_version --channels $channels $default_channel_opt

openshift_version_annotation_key="com.redhat.openshift.versions"
# Supports Openshift v4.14+ (https://redhat-connect.gitbook.io/certified-operator-guide/ocp-deployment/operator-metadata/bundle-directory/managing-openshift-versions)
openshift_supported_versions="v4.14"
key=$openshift_version_annotation_key value=$openshift_supported_versions yq --inplace '.annotations[strenv(key)] = strenv(value)' bundle/metadata/annotations.yaml
key=$openshift_version_annotation_key yq --inplace '(.annotations[strenv(key)] | key) headComment = "Custom annotations"' bundle/metadata/annotations.yaml

echo "reading data form quay.io, slow process."
dep_file="$env/bundle/metadata/dependencies.yaml"

limitador_version=$(mod_version $(yq '.dependencies.limitador-operator' $env/release.yaml))
limitador_image=quay.io/kuadrant/limitador-operator-bundle:$limitador_version
V=$(opm render $limitador_image | yq eval '.properties[] | select(.type == "olm.package") | .value.version' -)

COMPONENT=limitador-operator V=$V \
  yq eval '(.dependencies[] | select(.value.packageName == strenv(COMPONENT)).value.version) = strenv(V)' -i $dep_file


authorino_version=$(mod_version $(yq '.dependencies.authorino-operator' $env/release.yaml))
authorino_image=quay.io/kuadrant/authorino-operator-bundle:$authorino_version
V=$(opm render $authorino_image | yq eval '.properties[] | select(.type == "olm.package") | .value.version' -)

COMPONENT=authorino-operator V=$V \
  yq eval '(.dependencies[] | select(.value.packageName == strenv(COMPONENT)).value.version) = strenv(V)' -i $dep_file


dns_version=$(mod_version $(yq '.dependencies.dns-operator' $env/release.yaml))
dns_image=quay.io/kuadrant/dns-operator-bundle:$dns_version
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
