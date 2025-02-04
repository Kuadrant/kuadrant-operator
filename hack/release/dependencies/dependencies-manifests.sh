#!/usr/bin/env bash

authorino_bundle_version=v$(yq '.dependencies.Authorino_bundle' $env/release.toml)
dns_bundle_version=v$(yq '.dependencies.DNS_bundle' $env/release.toml)
limitador_bundle_version=v$(yq '.dependencies.Limitador_bundle' $env/release.toml)

AUTHORINO_OPERATOR_GITREF=$authorino_bundle_version envsubst < $env/config/dependencies/authorino/kustomization.template.yaml > $env/config/dependencies/authorino/kustomization.yaml

DNS_OPERATOR_GITREF=$dns_bundle_version envsubst < $env/config/dependencies/dns/kustomization.template.yaml > $env/config/dependencies/dns/kustomization.yaml

LIMITADOR_OPERATOR_GITREF=$limitador_bundle_version envsubst < $env/config/dependencies/limitador/kustomization.template.yaml > $env/config/dependencies/limitador/kustomization.yaml
