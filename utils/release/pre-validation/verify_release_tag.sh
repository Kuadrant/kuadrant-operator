#!/usr/bin/env bash

echo "Verifying if the Operator tag has been already released"
file=$env/release.yaml
kuadrant_operator_version=$(yq "(.kuadrant-operator.version)" "$file")

if [[ "$kuadrant_operator_version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*)(\.(0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*))*))?(\+([0-9a-zA-Z-]+(\.[0-9a-zA-Z-]+)*))?$ ]]; then
  if [[ $kuadrant_operator_version == "0.0.0" ]]; then
    echo "Version $kuadrant_operator_version is reserved and not valid for a release"
    exit 0
  fi
  echo "kuadrant-operator version $kuadrant_operator_version is valid semver"
else
  echo "kuadrant-operator version $kuadrant_operator_version is not valid semver"
  exit 1
fi

kuadrant_operator_tag="v$kuadrant_operator_version"

if [[ $GITHUB_TOKEN ]]; then
  response=$(curl -s -H "Authorization: Bearer $GITHUB_TOKEN" "https://api.github.com/repos/kuadrant/kuadrant-operator/tags")
else
  response=$(curl -s "https://api.github.com/repos/kuadrant/kuadrant-operator/tags")
fi

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
