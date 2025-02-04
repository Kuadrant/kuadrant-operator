#!/usr/bin/env bash

v=quay.io/kuadrant/kuadrant-operator-catalog:v$(yq '.kuadrant.release' $env/release.toml) \
	yq eval --inplace '.spec.image = strenv(v)' $env/config/deploy/olm/catalogsource.yaml	
