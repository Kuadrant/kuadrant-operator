#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${SCRIPT_DIR}/../bin"

KUSTOMIZE_BIN="${BIN_DIR}/kustomize"
KIND_BIN="${BIN_DIR}/kind"

function prepend() { while read line; do echo "${1}${line}"; done; }

CLUSTER_PREFIX="${CLUSTER_PREFIX:-kuadrant-local}"
CLUSTER_COUNT="${CLUSTER_COUNT:-1}"

## --- Test Setup Start --- ##

#TEST_NS_PREFIX="${TEST_NS_PREFIX:-dns-operator}"
TEST_NS_PREFIX="${TEST_NS_PREFIX:-dnstest}"
TEST_NS_COUNT="${TEST_NS_COUNT:-1}"

function upsertTestNamespace() {
  clusterRole=${1}
  nsName=${2}

  kubectl create namespace ${nsName} --dry-run=client -o yaml | kubectl apply -f -
  kubectl label namespace ${nsName} kuadrant.io/test=multicluster-poc
  kubectl label namespace ${nsName} kuadrant.io/cluster-role=${clusterRole}

  # Create gateway, application and dns provider secrets(if primary),
  kubectl apply -k ${SCRIPT_DIR}/${clusterRole} -n "${nsName}"
  kubectl wait --timeout=60s --for=condition=Available deployment --all -n "${nsName}"
}

function upsertTestNamespaces() {
   clusterRole=${1}
   for ((i = 1; i <= TEST_NS_COUNT; i++)); do
      nsName=${TEST_NS_PREFIX}-${i}
      echo " Creating namespace ${nsName}/${TEST_NS_COUNT}: ${nsName}"
      upsertTestNamespace "${clusterRole}" "${nsName}" | prepend "[${nsName}] "
    done
}

for ((i = 1; i <= CLUSTER_COUNT; i++)); do
  clusterName=${CLUSTER_PREFIX}-${i}
  clusterRole=$([ $i = 1 ] && echo "primary" || echo "remote")
  kubectl config use-context kind-${clusterName}
  upsertTestNamespaces "${clusterRole}" | prepend "[${clusterName}][${clusterRole}]"
done

## --- Test Setup End --- ##
