#!/usr/bin/env bash

mod_version() {
  version=$1
  if [ "$version" == "0.0.0" ]; then
    echo "main"
  else
    echo "v$version"
  fi
}

developerportal_version=$(mod_version $(yq '.dependencies.developer-portal-controller' $env/release.yaml))

DEVELOPERPORTAL_GITREF=$developerportal_version envsubst < $env/config/dependencies/developer-portal/kustomization.template.yaml > $env/config/dependencies/developer-portal/kustomization.yaml
