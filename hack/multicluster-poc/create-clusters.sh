#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${SCRIPT_DIR}/../../bin"

KIND_BIN="${BIN_DIR}/kind"

function prepend() { while read line; do echo "${1}${line}"; done; }

## --- Cluster Setup Start --- ##

CLUSTER_PREFIX="${CLUSTER_PREFIX:-kuadrant-local}"
CLUSTER_COUNT="${CLUSTER_COUNT:-1}"

cleanClusters() {
	# Delete existing kind clusters
	clusterCount=$(${KIND_BIN} get clusters | grep ${CLUSTER_PREFIX} | wc -l)
	if ! [[ $clusterCount =~ "0" ]] ; then
		echo "Deleting previous clusters."
		${KIND_BIN} get clusters | grep ${CLUSTER_PREFIX} | xargs ${KIND_BIN} delete clusters
	fi
}

make kind kustomize
#cleanClusters || true

KUBECONFIG_DIR="${SCRIPT_DIR}/kubeconfigs"

mkdir -p ${KUBECONFIG_DIR}

for ((i = 1; i <= CLUSTER_COUNT; i++)); do
  clusterName=${CLUSTER_PREFIX}-${i}
  if ${KIND_BIN} get clusters | grep ${clusterName} ; then
      echo "cluster ${i}/${CLUSTER_COUNT}: ${clusterName} already exists"
  else
    echo "Creating cluster ${i}/${CLUSTER_COUNT}: ${clusterName}"

    make local-setup KIND_CLUSTER_NAME=${clusterName} SUBNET_OFFSET=${i}| prepend "[${clusterName}] "
  fi

  echo "generating kubeconfigs" | prepend "[${clusterName}] "
  ${KIND_BIN} export kubeconfig -q -n ${clusterName} --kubeconfig ${KUBECONFIG_DIR}/${clusterName}.kubeconfig
  ${KIND_BIN} export kubeconfig -q -n ${clusterName} --kubeconfig ${KUBECONFIG_DIR}/kuadrant-local-all.kubeconfig
  ${KIND_BIN} export kubeconfig -q --internal -n ${clusterName} --kubeconfig ${KUBECONFIG_DIR}/${clusterName}.internal.kubeconfig
  ${KIND_BIN} export kubeconfig -q --internal -n ${clusterName} --kubeconfig ${KUBECONFIG_DIR}/kuadrant-local-all.internal.kubeconfig
done

## --- Cluster Setup End --- ##
