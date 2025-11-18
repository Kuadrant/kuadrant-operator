#!/usr/bin/env bash

mod_version() {
  version=$1
  if [ "$version" == "0.0.0" ]; then
    echo "main"
  else
    echo "v$version"
  fi
}

authorino_version=$(mod_version $(yq '.dependencies.authorino-operator' $env/release.yaml))
dns_version=$(mod_version $(yq '.dependencies.dns-operator' $env/release.yaml))
limitador_version=$(mod_version $(yq '.dependencies.limitador-operator' $env/release.yaml))
developerportal_version=$(mod_version $(yq '.dependencies.developer-portal-controller' $env/release.yaml))

AUTHORINO_OPERATOR_GITREF=$authorino_version envsubst < $env/config/dependencies/authorino/kustomization.template.yaml > $env/config/dependencies/authorino/kustomization.yaml

DNS_OPERATOR_GITREF=$dns_version envsubst < $env/config/dependencies/dns/kustomization.template.yaml > $env/config/dependencies/dns/kustomization.yaml

LIMITADOR_OPERATOR_GITREF=$limitador_version envsubst < $env/config/dependencies/limitador/kustomization.template.yaml > $env/config/dependencies/limitador/kustomization.yaml

DEVELOPERPORTAL_GITREF=$developerportal_version envsubst < $env/config/dependencies/developer-portal/kustomization.template.yaml > $env/config/dependencies/developer-portal/kustomization.yaml
