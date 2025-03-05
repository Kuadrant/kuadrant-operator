#!/usr/bin/env bash
mod_version() {
  version=$1
  if [ "$version" == "0.0.0" ]; then
    echo "latest"
  else
    echo "v$version"
  fi
}

v=quay.io/kuadrant/kuadrant-operator-catalog:$(mod_version $(yq '.kuadrant-operator.version' $env/release.yaml)) \
	yq eval --inplace '.spec.image = strenv(v)' $env/config/deploy/olm/catalogsource.yaml	
