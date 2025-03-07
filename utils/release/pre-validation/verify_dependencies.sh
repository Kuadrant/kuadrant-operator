#!/usr/bin/env bash

echo "Verifying if the dependencies have been already released"
file=$env/release.yaml
dependencies=("authorino-operator" "console-plugin" "dns-operator" "limitador-operator" "wasm-shim")

for dependency in "${dependencies[@]}"; do
  dependency_tag=v$(yq "(.dependencies.$dependency)" "$file")

  echo "Checking dependency $dependency tag $dependency_tag"

  # Get all tags and store raw response
  if [[ $dependency == "console-plugin" ]]; then
    response=$(curl -s -H "Authorization: Bearer $GITHUB_TOKEN" "https://api.github.com/repos/kuadrant/kuadrant-$dependency/tags")
  else
    response=$(curl -s -H "Authorization: Bearer $GITHUB_TOKEN" "https://api.github.com/repos/kuadrant/$dependency/tags")
  fi

  # Check if response contains valid JSON array
  if ! echo "$response" | jq empty > /dev/null 2>&1; then
    echo "Failed to fetch tags for $dependency"
    exit 1
  fi

  # Check if specific tag exists
  if [[ $( echo "$response" | jq 'any(.[]; .name == "'$dependency_tag'")' ) == false ]] ; then
    echo "$dependency tag $dependency_tag doesn't exist, stopping release process"
    exit 1
  fi
done

echo "All dependencies have been already released, continuing the release process"
