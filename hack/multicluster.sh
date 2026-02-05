#!/usr/bin/env bash

# Create multiple local Kind clusters
#
# Example:
# CLUSTER_COUNT=2 ./multicluster.sh local-setup
# CLUSTER_COUNT=2 ./multicluster.sh local-cleanup

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

ROOT_DIR="${SCRIPT_DIR}/.."
BIN_DIR="${ROOT_DIR}/bin"
KIND_BIN="${BIN_DIR}/kind"

CLUSTER_PREFIX="${CLUSTER_PREFIX:-kuadrant-local}"
CLUSTER_COUNT="${CLUSTER_COUNT:-1}"

## Location to generate cluster kubeconfigs
KUBECONFIGS_DIR=${ROOT_DIR}/tmp/kubeconfigs
KIND_ALL_KUBECONFIG=${KUBECONFIGS_DIR}/kuadrant-local-all.kubeconfig
KIND_ALL_INTERNAL_KUBECONFIG=${KUBECONFIGS_DIR}/kuadrant-local-all.internal.kubeconfig

function prepend() { while read line; do echo "${1}${line}"; done; }

## --- Local Setup Start --- ##

help() {
  echo "help"
}

local-cleanup() {
  for ((i = 1; i <= CLUSTER_COUNT; i++)); do
    clusterName=${CLUSTER_PREFIX}-${i}
    if ${KIND_BIN} get clusters | grep ${clusterName} ; then
        echo "Deleting cluster ${i}/${CLUSTER_COUNT}: ${clusterName}"
        (cd "${ROOT_DIR}" && make local-cleanup KIND_CLUSTER_NAME=${clusterName}) | prepend "[${clusterName}] "
    else
      echo "cluster ${i}/${CLUSTER_COUNT}: ${clusterName} not found"
    fi
    kubectl config delete-context kind-${clusterName} --kubeconfig ${KIND_ALL_INTERNAL_KUBECONFIG} || true
    kubectl config delete-cluster kind-${clusterName} --kubeconfig ${KIND_ALL_INTERNAL_KUBECONFIG} || true
    kubectl config delete-user kind-${clusterName} --kubeconfig ${KIND_ALL_INTERNAL_KUBECONFIG} || true
    kubectl config delete-context kind-${clusterName} --kubeconfig ${KIND_ALL_KUBECONFIG} || true
    kubectl config delete-cluster kind-${clusterName} --kubeconfig ${KIND_ALL_KUBECONFIG} || true
    kubectl config delete-user kind-${clusterName} --kubeconfig ${KIND_ALL_KUBECONFIG} || true
  done
}

local-setup() {
  for ((i = 1; i <= CLUSTER_COUNT; i++)); do
    clusterName=${CLUSTER_PREFIX}-${i}
    if ${KIND_BIN} get clusters | grep ${clusterName} ; then
        echo "cluster ${i}/${CLUSTER_COUNT}: ${clusterName} already exists"
    else
      echo "Creating cluster ${i}/${CLUSTER_COUNT}: ${clusterName}"
      (cd "${ROOT_DIR}" && make local-setup KIND_CLUSTER_NAME=${clusterName} SUBNET_OFFSET=${i}) | prepend "[${clusterName}] "
    fi
    kind export kubeconfig -q -n ${clusterName} --kubeconfig ${KIND_ALL_KUBECONFIG}
    kind export kubeconfig -q --internal -n ${clusterName} --kubeconfig ${KIND_ALL_INTERNAL_KUBECONFIG}
  done
}

## --- Local Setup End --- ##

create-cluster-secret() {
  cp "$(which kubectl_kuadrant-dns)" ${ROOT_DIR}/tmp/
  kubectl config use-context ${1} --kubeconfig ${ROOT_DIR}/tmp/kubeconfigs/kuadrant-local-all.internal.kubeconfig
  docker run --rm -v ${ROOT_DIR}:/tmp/dns-operator:z --network kind \
    -e KUBECONFIG=/tmp/dns-operator/tmp/kubeconfigs/kuadrant-local-all.internal.kubeconfig alpine/k8s:1.30.13 \
    /tmp/dns-operator/tmp/kubectl_kuadrant-dns add-cluster-secret --context ${2} --namespace kuadrant-system
}

f_call=${1-help}; shift; $f_call "$@"
