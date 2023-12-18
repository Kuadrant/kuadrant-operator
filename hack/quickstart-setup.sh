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

if [ -z $KUADRANT_ORG ]; then
  KUADRANT_ORG=${KUADRANT_ORG:="kuadrant"}
fi
if [ -z $KUADRANT_REF ]; then
  KUADRANT_REF=${KUADRANT_REF:="main"}
fi
if [ -z $MGC_REF ]; then
  MGC_REF=${MGC_REF:="main"}
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source /dev/stdin <<< "$(curl -s https://raw.githubusercontent.com/${KUADRANT_ORG}/multicluster-gateway-controller/${MGC_REF}/hack/.quickstartEnv)"
source /dev/stdin <<< "$(curl -s https://raw.githubusercontent.com/${KUADRANT_ORG}/multicluster-gateway-controller/${MGC_REF}/hack/.deployUtils)"

KUADRANT_IMAGE="quay.io/${KUADRANT_ORG}/kuadrant-operator:latest"
KUADRANT_REPO="github.com/${KUADRANT_ORG}/kuadrant-operator.git"
KUADRANT_REPO_RAW="https://raw.githubusercontent.com/${KUADRANT_ORG}/kuadrant-operator/${KUADRANT_REF}"
KUADRANT_DEPLOY_KUSTOMIZATION="${KUADRANT_REPO}/config/deploy"
KUADRANT_GATEWAY_API_KUSTOMIZATION="${KUADRANT_REPO}/config/dependencies/gateway-api"
KUADRANT_ISTIO_KUSTOMIZATION="${KUADRANT_REPO}/config/dependencies/istio/sail"
KUADRANT_CERT_MANAGER_KUSTOMIZATION="${KUADRANT_REPO}/config/dependencies/cert-manager"
KUADRANT_METALLB_KUSTOMIZATION="${KUADRANT_REPO}/config/metallb"

set -e pipefail

if [[ "${KUADRANT_REF}" != "main" ]]; then
  echo "setting KUADRANT_REPO to use branch ${KUADRANT_REF}"
  KUADRANT_IMAGE="quay.io/${KUADRANT_ORG}/kuadrant-operator:${KUADRANT_REF}"
  KUADRANT_GATEWAY_API_KUSTOMIZATION=${KUADRANT_GATEWAY_API_KUSTOMIZATION}?ref=${KUADRANT_REF}
  KUADRANT_ISTIO_KUSTOMIZATION=${KUADRANT_ISTIO_KUSTOMIZATION}?ref=${KUADRANT_REF}
  KUADRANT_CERT_MANAGER_KUSTOMIZATION=${KUADRANT_CERT_MANAGER_KUSTOMIZATION}?ref=${KUADRANT_REF}
  KUADRANT_METALLB_KUSTOMIZATION=${KUADRANT_METALLB_KUSTOMIZATION}?ref=${KUADRANT_REF}
fi

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
${KIND_BIN} create cluster --name ${KUADRANT_CLUSTER_NAME} --config=- <<< "$(curl -s ${KUADRANT_REPO_RAW}/utils/kind-cluster.yaml)"
kubectl config use-context kind-${KUADRANT_CLUSTER_NAME}

# Create namespace
kubectl create namespace ${KUADRANT_NAMESPACE}

# Install gateway api
echo "Installing Gateway API in ${KUADRANT_CLUSTER_NAME}"
${KUSTOMIZE_BIN} build ${KUADRANT_GATEWAY_API_KUSTOMIZATION} | kubectl apply -f -

# Install istio
echo "Installing Istio in ${KUADRANT_CLUSTER_NAME}"
${KUSTOMIZE_BIN} build ${KUADRANT_ISTIO_KUSTOMIZATION} | kubectl apply -f -
kubectl -n istio-system wait --for=condition=Available deployment istio-operator --timeout=300s
kubectl apply -f ${KUADRANT_REPO_RAW}/config/dependencies/istio/sail/istio.yaml

# Install cert-manager
echo "Installing cert-manager in ${KUADRANT_CLUSTER_NAME}"
${KUSTOMIZE_BIN} build ${KUADRANT_CERT_MANAGER_KUSTOMIZATION} | kubectl apply -f -
echo "Waiting for cert-manager deployments to be ready"
kubectl -n cert-manager wait --for=condition=Available deployments --all --timeout=300s

# Install metallb
echo "Installing metallb in ${KUADRANT_CLUSTER_NAME}"
${KUSTOMIZE_BIN} build ${KUADRANT_METALLB_KUSTOMIZATION} | kubectl apply -f -
echo "Waiting for metallb-system deployments to be ready"
kubectl -n metallb-system wait --for=condition=Available deployments controller --timeout=300s
kubectl -n metallb-system wait --for=condition=ready pod --selector=app=metallb --timeout=60s
kubectl apply -n metallb-system -f - <<< "$(curl -s ${KUADRANT_REPO_RAW}/utils/docker-network-ipaddresspool.sh | bash -s -- kind)"

# Install kuadrant
echo "Installing Kuadrant in ${KUADRANT_CLUSTER_NAME}"
${KUSTOMIZE_BIN} build ${KUADRANT_DEPLOY_KUSTOMIZATION} | kubectl apply -f -
kubectl -n ${KUADRANT_NAMESPACE} patch deployment kuadrant-operator-controller-manager --type='merge' -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","image":"'"${KUADRANT_IMAGE}"'"}]}}}}'

# Configure managedzone
if [ ! -z "$DNS_PROVIDER" ]; then
  configureController ${KUADRANT_CLUSTER_NAME} ${KUADRANT_NAMESPACE}
fi

# Deploy kuadrant
kubectl -n ${KUADRANT_NAMESPACE} apply -f ${KUADRANT_REPO_RAW}/config/samples/kuadrant_v1beta1_kuadrant.yaml
echo "You are now set up to follow the quick start guide at https://docs.kuadrant.io/kuadrant-operator/doc/user-guides/secure-protect-connect/"
