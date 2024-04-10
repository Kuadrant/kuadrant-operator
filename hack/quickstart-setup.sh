#!/bin/bash

#
# Copyright 2021 Red Hat, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

set -e pipefail

check_dependencies() {
  # Check for Docker or Podman
  if ! command -v docker &>/dev/null && ! command -v podman &>/dev/null; then
    echo "Error: neither docker nor podman could be found. Please install docker or podman."
    exit 1
  fi

  # Check for other dependencies
  for cmd in kind kubectl; do
    if ! command -v $cmd &>/dev/null; then
      echo "Error: $cmd could not be found. Please install $cmd."
      exit 1
    fi
  done
}

check_dependencies

if [ -z $KUADRANT_ORG ]; then
  KUADRANT_ORG=${KUADRANT_ORG:="kuadrant"}
fi
if [ -z $KUADRANT_REF ]; then
  KUADRANT_REF=${KUADRANT_REF:="main"}
fi
if [ -z $MGC_REF ]; then
  MGC_REF=${MGC_REF:="main"}
fi

if [ -z $ISTIO_INSTALL_SAIL ]; then
  ISTIO_INSTALL_SAIL=${ISTIO_INSTALL_SAIL:=false}
fi

export TOOLS_IMAGE=quay.io/kuadrant/mgc-tools:latest
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
export TMP_DIR=$SCRIPT_DIR/tmp/mgc

# Generate MetalLB IpAddressPool for a given network
generate_ip_address_pool() {
  set -euo pipefail

  networkName=$1
  YQ="${2:-yq}"

  ## Parse kind network subnet
  ## Take only IPv4 subnets, exclude IPv6
  SUBNET=""

  # Try podman version of cmd first. docker alias may be used for podman, so network
  # command will be different
  set +e
  if command -v podman &>/dev/null; then
    SUBNET=$(podman network inspect -f '{{range .Subnets}}{{if eq (len .Subnet.IP) 4}}{{.Subnet}}{{end}}{{end}}' $networkName)
  fi
  set -e

  # Fallback to docker version of cmd
  if [[ -z "$SUBNET" ]]; then
    SUBNET=$(docker network inspect $networkName -f '{{ (index .IPAM.Config 0).Subnet }}')
  fi

  # Neither worked, error out
  if [[ -z "$SUBNET" ]]; then
    echo "Error: parsing IPv4 network address for '$networkName' docker network"
    exit 1
  fi

  # shellcheck disable=SC2206
  subnetParts=(${SUBNET//./ })
  cidr="${subnetParts[0]}.${subnetParts[1]}.0.252/30"

  cat <<EOF | ADDRESS=$cidr ${YQ} '(select(.kind == "IPAddressPool") | .spec.addresses[0]) = env(ADDRESS)'
---
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: kuadrant-local
spec:
  addresses: [] # set by make target
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: empty
EOF
}

containerRuntime() {
  local container_runtime=""
  if command -v podman &>/dev/null; then
    container_runtime="podman"
  elif command -v docker &>/dev/null; then
    container_runtime="docker"
  else
    echo "Neither Docker nor Podman is installed. Exiting..."
    exit 1
  fi
  echo "$container_runtime"
}

export CONTAINER_RUNTIME_BIN=$(containerRuntime)

dockerBinCmd() {
  local network=""
  if [ ! -z "${KIND_CLUSTER_DOCKER_NETWORK}" ]; then
    network=" --network ${KIND_CLUSTER_DOCKER_NETWORK}"
  fi

  echo "$CONTAINER_RUNTIME_BIN run --rm -u $UID -v ${TMP_DIR}:${TMP_DIR}${network} -e KUBECONFIG=${TMP_DIR}/kubeconfig --entrypoint=$1 $TOOLS_IMAGE"
}

export KIND_BIN=kind
export HELM_BIN=helm
export KUSTOMIZE_BIN=$(dockerBinCmd "kustomize")

requiredENV() {
  echo "Enter which DNS provider you will be using (gcp/aws)"
  read PROVIDER </dev/tty
  if [[ "$PROVIDER" =~ ^(gcp|aws)$ ]]; then
    echo "Provider chosen: $PROVIDER."
    export DNS_PROVIDER=$PROVIDER
  else
    echo "Invalid input given. Please enter either 'gcp' or 'aws' (case sensitive)."
    exit 1
  fi

  if [[ "$PROVIDER" == "aws" ]]; then
    if [[ -z "${KUADRANT_AWS_ACCESS_KEY_ID}" ]]; then
      echo "Enter an AWS access key ID for an account where you have access to Route53:"
      read KUADRANT_AWS_ACCESS_KEY_ID </dev/tty
      echo "export KUADRANT_AWS_ACCESS_KEY_ID for future executions of the script to skip this step"
    fi

    if [[ -z "${KUADRANT_AWS_SECRET_ACCESS_KEY}" ]]; then
      echo "Enter the corresponding AWS secret access key for the AWS access key ID entered above:"
      read KUADRANT_AWS_SECRET_ACCESS_KEY </dev/tty
      echo "export KUADRANT_AWS_SECRET_ACCESS_KEY for future executions of the script to skip this step"
    fi

    if [[ -z "${KUADRANT_AWS_REGION}" ]]; then
      echo "Enter an AWS region (e.g. eu-west-1) for an Account where you have access to Route53:"
      read KUADRANT_AWS_REGION </dev/tty
      echo "export KUADRANT_AWS_REGION for future executions of the script to skip this step"
    fi

    if [[ -z "${KUADRANT_AWS_DNS_PUBLIC_ZONE_ID}" ]]; then
      echo "Enter the Public Zone ID of your Route53 zone:"
      read KUADRANT_AWS_DNS_PUBLIC_ZONE_ID </dev/tty
      echo "export KUADRANT_AWS_DNS_PUBLIC_ZONE_ID for future executions of the script to skip this step"
    fi

    if [[ -z "${KUADRANT_ZONE_ROOT_DOMAIN}" ]]; then
      echo "Enter the root domain of your Route53 hosted zone (e.g. www.example.com):"
      read KUADRANT_ZONE_ROOT_DOMAIN </dev/tty
      echo "export KUADRANT_ZONE_ROOT_DOMAIN for future executions of the script to skip this step"
    fi
  else
    if [[ -z "${GOOGLE}" ]]; then
      echo "Enter either credentials created either by CLI or by service account (Please make sure the credentials provided are in JSON format)"
      read GOOGLE </dev/tty
      echo "export GOOGLE for future executions of the script to skip this step"
    fi
    if ! jq -e . <<<"$GOOGLE" >/dev/null 2>&1; then
      echo "Credentials provided is not in JSON format"
      exit 1
    fi

    if [[ -z "${PROJECT_ID}" ]]; then
      echo "Enter the project id for your GCP Cloud DNS:"
      read PROJECT_ID </dev/tty
      echo "export PROJECT_ID for future executions of the script to skip this step"
    fi

    if [[ -z "${ZONE_DNS_NAME}" ]]; then
      echo "Enter the DNS name for your GCP Cloud DNS:"
      read ZONE_DNS_NAME </dev/tty
      echo "export ZONE_DNS_NAME for future executions of the script to skip this step"
    fi

    if [[ -z "${ZONE_NAME}" ]]; then
      echo "Enter the Zone name for your GCP Cloud DNS:"
      read ZONE_NAME </dev/tty
      echo "export ZONE_NAME for future executions of the script to skip this step"
    fi
  fi
}

configureController() {
  postDeployMGCHub ${1} ${2}
}

postDeployMGCHub() {
  clusterName=${1}
  namespace=${2}
  kubectl config use-context kind-${clusterName}
  echo "Running post MGC deployment setup on ${clusterName}"

  case $DNS_PROVIDER in
  aws)
    echo "Setting up an AWS Route 53 DNS provider"
    setupAWSProvider ${namespace}
    ;;
  gcp)
    echo "Setting up a Google Cloud DNS provider"
    setupGCPProvider ${namespace}
    ;;
  *)
    echo "Unknown DNS provider"
    exit
    ;;
  esac
}
# shellcheck shell=bash

# Shared functions between local-setup-mgc and quickstart-setup script

configureMetalLB() {
  clusterName=${1}
  metalLBSubnet=${2}

  kubectl config use-context kind-${clusterName}
  echo "Creating MetalLB AddressPool"
  cat <<EOF | kubectl apply -f -
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: example
  namespace: metallb-system
spec:
  addresses:
  - 172.31.${metalLBSubnet}.0/24
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: empty
  namespace: metallb-system
EOF
}

# quickstart-setup specific functions

setupAWSProvider() {
  local namespace="$1"
  if [ -z "$1" ]; then
    namespace="multi-cluster-gateways"
  fi
  if [ "$KUADRANT_AWS_ACCESS_KEY_ID" == "" ]; then
    echo "KUADRANT_AWS_ACCESS_KEY_ID is not set"
    exit 1
  fi

  kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: ${KIND_CLUSTER_PREFIX}aws-credentials
  namespace: ${namespace}
type: "kuadrant.io/aws"
stringData:
  AWS_ACCESS_KEY_ID: ${KUADRANT_AWS_ACCESS_KEY_ID}
  AWS_SECRET_ACCESS_KEY: ${KUADRANT_AWS_SECRET_ACCESS_KEY}
  AWS_REGION: ${KUADRANT_AWS_REGION}
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${KIND_CLUSTER_PREFIX}controller-config
  namespace: ${namespace}
data:
  AWS_DNS_PUBLIC_ZONE_ID: ${KUADRANT_AWS_DNS_PUBLIC_ZONE_ID}
  ZONE_ROOT_DOMAIN: ${KUADRANT_ZONE_ROOT_DOMAIN}
  LOG_LEVEL: "${LOG_LEVEL}"
---
apiVersion: kuadrant.io/v1alpha1
kind: ManagedZone
metadata:
  name: ${KIND_CLUSTER_PREFIX}dev-mz
  namespace: ${namespace}
spec:
  id: ${KUADRANT_AWS_DNS_PUBLIC_ZONE_ID}
  domainName: ${KUADRANT_ZONE_ROOT_DOMAIN}
  description: "Dev Managed Zone"
  dnsProviderSecretRef:
    name: ${KIND_CLUSTER_PREFIX}aws-credentials
EOF
}

setupGCPProvider() {
  local namespace="$1"
  if [ -z "$1" ]; then
    namespace="multi-cluster-gateways"
  fi
  kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: ${KIND_CLUSTER_PREFIX}gcp-credentials
  namespace: ${namespace}
type: "kuadrant.io/gcp"
stringData:
  GOOGLE: '${GOOGLE}'
  PROJECT_ID: ${PROJECT_ID}
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${KIND_CLUSTER_PREFIX}controller-config
  namespace: ${namespace}
data:
  ZONE_DNS_NAME: ${ZONE_DNS_NAME}
  ZONE_NAME: ${ZONE_NAME}
  LOG_LEVEL: "${LOG_LEVEL}"
---
apiVersion: kuadrant.io/v1alpha1
kind: ManagedZone
metadata:
  name: ${KIND_CLUSTER_PREFIX}dev-mz
  namespace: ${namespace}
spec:
  id: ${ZONE_NAME}
  domainName: ${ZONE_DNS_NAME}
  description: "Dev Managed Zone"
  dnsProviderSecretRef:
    name: ${KIND_CLUSTER_PREFIX}gcp-credentials
EOF
}

LOCAL_SETUP_DIR="$(dirname "${BASH_SOURCE[0]}")"

YQ_BIN=$(dockerBinCmd "yq")

KUADRANT_REPO="github.com/${KUADRANT_ORG}/kuadrant-operator.git"
KUADRANT_REPO_RAW="https://raw.githubusercontent.com/${KUADRANT_ORG}/kuadrant-operator/${KUADRANT_REF}"
KUADRANT_DEPLOY_KUSTOMIZATION="${KUADRANT_REPO}/config/deploy?ref=${KUADRANT_REF}"
KUADRANT_GATEWAY_API_KUSTOMIZATION="${KUADRANT_REPO}/config/dependencies/gateway-api?ref=${KUADRANT_REF}"
KUADRANT_ISTIO_KUSTOMIZATION="${KUADRANT_REPO}/config/dependencies/istio/sail?ref=${KUADRANT_REF}"
KUADRANT_CERT_MANAGER_KUSTOMIZATION="${KUADRANT_REPO}/config/dependencies/cert-manager?ref=${KUADRANT_REF}"
KUADRANT_METALLB_KUSTOMIZATION="${KUADRANT_REPO}/config/metallb?ref=${KUADRANT_REF}"
MGC_REPO="github.com/${KUADRANT_ORG}/multicluster-gateway-controller.git"
MGC_ISTIO_KUSTOMIZATION="${MGC_REPO}/config/istio?ref=${MGC_REF}"

# Make temporary directory
mkdir -p ${TMP_DIR}

KUADRANT_CLUSTER_NAME=kuadrant-local
KUADRANT_NAMESPACE=kuadrant-system

echo "Do you want to set up a DNS provider? (y/N)"
read SETUP_PROVIDER </dev/tty
if [[ "$SETUP_PROVIDER" =~ ^[yY]$ ]]; then
  requiredENV
fi

# Kind delete cluster
${KIND_BIN} delete cluster --name ${KUADRANT_CLUSTER_NAME}

# Kind create cluster
${KIND_BIN} create cluster --name ${KUADRANT_CLUSTER_NAME} --config=- <<<"$(curl -s ${KUADRANT_REPO_RAW}/utils/kind-cluster.yaml)"
kubectl config use-context kind-${KUADRANT_CLUSTER_NAME}

# Create namespace
kubectl create namespace ${KUADRANT_NAMESPACE}

# Install gateway api
echo "Installing Gateway API in ${KUADRANT_CLUSTER_NAME}"
kubectl apply -k ${KUADRANT_GATEWAY_API_KUSTOMIZATION}

# Install istio
echo "Installing Istio in ${KUADRANT_CLUSTER_NAME}"
if [ "$ISTIO_INSTALL_SAIL" = true ]; then
  echo "Installing via Sail"
  kubectl apply -k ${KUADRANT_ISTIO_KUSTOMIZATION}
  kubectl -n istio-system wait --for=condition=Available deployment istio-operator --timeout=300s
  kubectl apply -f ${KUADRANT_REPO_RAW}/config/dependencies/istio/sail/istio.yaml
else
  # Create CRD first to prevent race condition with creating CR
  echo "Installing without Sail"
  kubectl kustomize ${MGC_ISTIO_KUSTOMIZATION} | tee ${TMP_DIR}/doctmp
  ${YQ_BIN} 'select(.kind == "CustomResourceDefinition")' ${TMP_DIR}/doctmp | kubectl apply -f -
  kubectl -n istio-system wait --for=condition=established crd/istiooperators.install.istio.io --timeout=60s
  cat ${TMP_DIR}/doctmp | kubectl apply -f -
  kubectl -n istio-operator wait --for=condition=Available deployment istio-operator --timeout=300s
fi

# Install cert-manager
echo "Installing cert-manager in ${KUADRANT_CLUSTER_NAME}"
kubectl apply -k ${KUADRANT_CERT_MANAGER_KUSTOMIZATION}
echo "Waiting for cert-manager deployments to be ready"
kubectl -n cert-manager wait --for=condition=Available deployments --all --timeout=300s

# Install metallb
echo "Installing metallb in ${KUADRANT_CLUSTER_NAME}"
kubectl apply -k ${KUADRANT_METALLB_KUSTOMIZATION}
echo "Waiting for metallb-system deployments to be ready"
kubectl -n metallb-system wait --for=condition=Available deployments controller --timeout=300s
kubectl -n metallb-system wait --for=condition=ready pod --selector=app=metallb --timeout=60s
generate_ip_address_pool "kind" | kubectl apply -n metallb-system -f -

# Install kuadrant
echo "Installing Kuadrant in ${KUADRANT_CLUSTER_NAME}"
kubectl apply -k ${KUADRANT_DEPLOY_KUSTOMIZATION} --server-side --validate=false

# Configure managedzone
if [ ! -z "$DNS_PROVIDER" ]; then
  configureController ${KUADRANT_CLUSTER_NAME} ${KUADRANT_NAMESPACE}
fi

# Deploy kuadrant
kubectl -n ${KUADRANT_NAMESPACE} apply -f ${KUADRANT_REPO_RAW}/config/samples/kuadrant_v1beta1_kuadrant.yaml
echo "You are now set up to follow the quick start guide at https://docs.kuadrant.io/kuadrant-operator/doc/user-guides/secure-protect-connect/"
