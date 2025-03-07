#!/usr/bin/env bash

echo "Verifying if the Operator tag has been already released"
file=$env/release.yaml
kuadrant_operator_tag=v$(yq "(.kuadrant-operator.version)" "$file")

response=$(curl -s https://api.github.com/repos/kuadrant/kuadrant-operator/tags | jq 'any(.[]; .name == "'$kuadrant_operator_tag'")')

if [[ $response == "true"  ]]; then
  echo "Kuadrant operator tag $kuadrant_operator_tag already exists, stopping release process"
  exit 1
fi
