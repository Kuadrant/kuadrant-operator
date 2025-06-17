#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

kubectl config use-context kind-kuadrant-local-1

curl -s https://raw.githubusercontent.com/Kuadrant/dns-operator/refs/heads/multicluster-poc/hack/create-kubeconfig-secret.sh | bash -s - -c kind-kuadrant-local-2 -a dns-operator-controller-manager -n kuadrant-system
