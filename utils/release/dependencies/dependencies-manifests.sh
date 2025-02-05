#!/usr/bin/env bash

authorino_version=v$(yq '.dependencies.authorino-operator' $env/release.yaml)
dns_version=v$(yq '.dependencies.dns-operator' $env/release.yaml)
limitador_version=v$(yq '.dependencies.limitador-operator' $env/release.yaml)

AUTHORINO_OPERATOR_GITREF=$authorino_version envsubst < $env/config/dependencies/authorino/kustomization.template.yaml > $env/config/dependencies/authorino/kustomization.yaml

DNS_OPERATOR_GITREF=$dns_version envsubst < $env/config/dependencies/dns/kustomization.template.yaml > $env/config/dependencies/dns/kustomization.yaml

LIMITADOR_OPERATOR_GITREF=$limitador_version envsubst < $env/config/dependencies/limitador/kustomization.template.yaml > $env/config/dependencies/limitador/kustomization.yaml
