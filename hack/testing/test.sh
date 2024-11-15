#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

BIN_DIR="${SCRIPT_DIR}/../../bin"
KIND_BIN="${BIN_DIR}/kind"
KUSTOMIZE_BIN="${BIN_DIR}/kustomize"

CLUSTER_PREFIX="${CLUSTER_PREFIX:-kuadrant-local}"
CLUSTER_COUNT="${CLUSTER_COUNT:-1}"

function prepend() { while read line; do echo "${1}${line}"; done; }

if ! command -v timeout &> /dev/null
then
    echo "'timeout' command not found."
    if [[ "$OSTYPE" == "darwin"* ]]; then
        echo "Try 'brew install coreutils'"
    fi
    exit
fi

wait_for() {
  local command="${1}"
  local description="${2}"
  local timeout="${3}"
  local interval="${4}"

  printf "Waiting for %s for %s...\n" "${description}" "${timeout}"
  timeout --foreground "${timeout}" bash -c "
    until ${command}
    do
        printf \"Waiting for %s... Trying again in ${interval}s\n\" \"${description}\"
        sleep ${interval}
    done
    "
  printf "%s finished!\n" "${description}"
}

## --- Cluster Setup Start --- ##

help() {
  echo "help"
}

init() {
  make kind kustomize
}

kind-delete() {
	# Delete existing kind clusters
	clusterCount=$(${KIND_BIN} get clusters | grep ${CLUSTER_PREFIX} | wc -l)
	if ! [[ $clusterCount =~ "0" ]] ; then
		echo "Deleting previous clusters."
		${KIND_BIN} get clusters | grep ${CLUSTER_PREFIX} | xargs ${KIND_BIN} delete clusters
	fi
}

kind-create() {
  for ((i = 1; i <= CLUSTER_COUNT; i++)); do
    clusterName=${CLUSTER_PREFIX}-${i}
    if ${KIND_BIN} get clusters | grep ${clusterName} ; then
        echo "cluster ${i}/${CLUSTER_COUNT}: ${clusterName} already exists"
    else
      echo "Creating cluster ${i}/${CLUSTER_COUNT}: ${clusterName}"
      make kind-create-cluster install-metallb KIND_CLUSTER_NAME=${clusterName} SUBNET_OFFSET=${i}| prepend "[${clusterName}] "
    fi
  done
}

generate-cluster-overlay() {
  for ((i = 1; i <= CLUSTER_COUNT; i++)); do
    clusterName=${CLUSTER_PREFIX}-${i}
    make generate-cluster-overlay USE_REMOTE_CONFIG=false KIND_CLUSTER_NAME=${clusterName}| prepend "[${clusterName}] "
  done
}

apply-cluster-overlay() {
  for ((i = 1; i <= CLUSTER_COUNT; i++)); do
    clusterName=${CLUSTER_PREFIX}-${i}
    wait_for "${KUSTOMIZE_BIN} build "${SCRIPT_DIR}/../../tmp/overlays/${clusterName}" --enable-helm | kubectl apply --context kind-${clusterName} --server-side -f -" "cluster config apply" "1m" "5"| prepend "[${clusterName}] "
  done
}

## --- Cluster Setup End --- ##

## --- Test Setup Start --- ##

TEST_NS_PREFIX="${TEST_NS_PREFIX:-testns}"
TEST_NS_COUNT="${TEST_NS_COUNT:-1}"

function upsertTestNamespace() {
  nsName=${1}

  #Create namespace if it doesn't already exist
  kubectl create namespace ${nsName} --dry-run=client -o yaml | kubectl apply -f -

  # Apply test configuration if function supplied
  if [ -n "$2" ]; then
    echo "Apply test function ${2}"
    f_call=$2; shift; shift; $f_call "$nsName" "$@" | prepend "[${f_call}] "
  fi
}

function upsertTestNamespaces() {
  for ((i = 1; i <= TEST_NS_COUNT; i++)); do
    nsName=${TEST_NS_PREFIX}-${i}
    echo "Creating namespace ${nsName}/${TEST_NS_COUNT}: ${nsName}"
    upsertTestNamespace "${nsName}" "$@" | prepend "[${nsName}] "
  done
}

function upsertClusterTestNamespaces() {
  for ((i = 1; i <= CLUSTER_COUNT; i++)); do
    clusterName=${CLUSTER_PREFIX}-${i}
    kubectl config use-context kind-${clusterName}
    upsertTestNamespaces "$@" | prepend "[${clusterName}] "
  done
}

# Create common test namespace configuration i.e. dns provider secrets, gateway and application
function apply_common() {
  echo "apply_common namespace: ${1}"
  lblSelector=${2-kuadrant.io/test-suite=manual}
  kubectl apply -k ${SCRIPT_DIR}/config/test-namespace/echo-app -n "${1}"
  kubectl apply -k ${SCRIPT_DIR}/config/test-namespace -n "${1}" -l ${lblSelector}
  kubectl wait --timeout=60s --for=condition=Available deployment --all -n "${1}"
}

function apply_dnspolicy_simple() {
  echo "apply_dnspolicy_simple namespace: ${1}, dns-provider: ${2}"
  kubectl apply -k ${SCRIPT_DIR}/config/test-namespace/dnspolicy/simple -l kuadrant.io/test-dns-provider=${2} -n "${1}"
}

function apply_dnspolicy_loadbalanced() {
  echo "apply_dnspolicy_loadbalanced namespace: ${1}, dns-provider: ${2}"
  kubectl apply -k ${SCRIPT_DIR}/config/test-namespace/dnspolicy/loadbalanced -l kuadrant.io/test-dns-provider=${2} -n "${1}"
}

function test_dnspolicy_simple() {
  dnsProvider=${1-inmemory}
  upsertClusterTestNamespaces apply_common kuadrant.io/test-dns-provider=${dnsProvider}
  upsertClusterTestNamespaces apply_dnspolicy_simple ${dnsProvider}
  kubectl wait --timeout=60s --for=condition=Accepted dnspolicy -l kuadrant.io/test=dnspolicy_prod-web-istio-simple -A
  kubectl wait --timeout=60s --for=condition=Enforced dnspolicy -l kuadrant.io/test=dnspolicy_prod-web-istio-simple -A
}

function test_dnspolicy_loadbalanced() {
  dnsProvider=${1-inmemory}
  upsertClusterTestNamespaces apply_common kuadrant.io/test-dns-provider=${dnsProvider}
  upsertClusterTestNamespaces apply_dnspolicy_loadbalanced ${dnsProvider}
  kubectl wait --timeout=60s --for=condition=Accepted dnspolicy -l kuadrant.io/test=dnspolicy_prod-web-istio-loadbalanced -A
  kubectl wait --timeout=60s --for=condition=Enforced dnspolicy -l kuadrant.io/test=dnspolicy_prod-web-istio-loadbalanced -A
}

## --- Test Setup End --- ##

f_call=${1-help}; shift; $f_call "$@"
