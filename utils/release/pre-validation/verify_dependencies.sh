#!/usr/bin/env bash

echo "Verifying if the dependencies have been already released"
file=$env/release.yaml
dependencies=("authorino-operator" "console-plugin" "dns-operator" "limitador-operator" "wasm-shim")

auth_header=""
if [[ $GITHUB_TOKEN ]]; then
  auth_header="-H \"Authorization: Bearer $GITHUB_TOKEN\""
fi

for dependency in "${dependencies[@]}"; do
  dependency_version=$(yq "(.dependencies.$dependency)" "$file")

  if [[ "$dependency_version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*)(\.(0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*))*))?(\+([0-9a-zA-Z-]+(\.[0-9a-zA-Z-]+)*))?$ ]]; then
    if [[ $dependency_version == "0.0.0" ]]; then
      echo "$dependency version $dependency_version is reserved and not valid for a release"
      exit 0
    fi
    echo "$dependency version $dependency_version is valid semver"
  else
    echo "$dependency version $dependency_version is not valid semver"
    exit 1
  fi

  dependency_tag="v$dependency_version"

  echo "Checking dependency $dependency tag $dependency_tag"

  url="https://api.github.com/repos/kuadrant/$dependency/tags"

  if [[ $dependency == "console-plugin" ]]; then
    url="https://api.github.com/repos/kuadrant/kuadrant-$dependency/tags"
  fi

  # Get all tags and store raw response
  response=$(curl -s $auth_header "$url")

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
