#!/usr/bin/env bash

echo "Verifying if the Operator tag has been already released"
file=$env/release.yaml
kuadrant_operator_tag=v$(yq "(.kuadrant-operator.version)" "$file")

response=$(curl -s -H "Authorization: Bearer $GITHUB_TOKEN" "https://api.github.com/repos/kuadrant/kuadrant-operator/tags")

# Check if response contains valid JSON array
if ! echo "$response" | jq empty > /dev/null 2>&1; then
  echo "Failed to fetch tags for kuadrant-operator"
  exit 1
fi

# Check if specific tag exists
if [[ $( echo "$response" | jq 'any(.[]; .name == "'$kuadrant_operator_tag'")' ) == true ]]; then
  echo "kuadrant-operator tag $kuadrant_operator_tag already exists, stopping the release process"
  exit 1
else
  echo "kuadrant-operator tag $kuadrant_operator_tag doesn't exists, continuing the release process"
fi
