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

set -euo pipefail

export KUADRANT_NAMESPACE="kuadrant-system"
export KIND_CLUSTER_NAME="kuadrant-local"
kind create cluster

echo "Building kuadrant"
docker build -t kuadrant:devel ./
kind load docker-image kuadrant:devel --name ${KIND_CLUSTER_NAME}

echo "Creating namespace"
kubectl create namespace "${KUADRANT_NAMESPACE}"

echo "Deploying Ingress Provider"
kubectl apply -f utils/local-deployment/istio-manifests/Base/Base.yaml
kubectl apply -f utils/local-deployment/istio-manifests/Base/Pilot/Pilot.yaml
kubectl apply -f utils/local-deployment/istio-manifests/Base/Pilot/IngressGateways/IngressGateways.yaml
kubectl apply -n "${KUADRANT_NAMESPACE}" -f utils/local-deployment/istio-manifests/default-gateway.yaml

echo "Deploying Kuadrant control plane"
kustomize build config/default | kubectl -n "${KUADRANT_NAMESPACE}" apply -f -
kubectl -n "${KUADRANT_NAMESPACE}" patch deployment kuadrant-controller-manager -p '{"spec": {"template": {"spec":{"containers":[{"name": "manager","image":"kuadrant:devel", "imagePullPolicy":"IfNotPresent"}]}}}}'

echo "Deploying EchoAPI to the default namespace"
kubectl apply -n default -f utils/local-deployment/echo-api.yaml

echo "Wait for all deployments to be up"
kubectl -n "${KUADRANT_NAMESPACE}" wait --timeout=300s --for=condition=Available deployments --all

echo
echo "Now you can export the kuadrant gateway by doing:"
echo "kubectl port-forward --namespace ${KUADRANT_NAMESPACE} deployment/kuadrant-gateway 8080:8080 8443:8443"
echo "after that, you can curl -H \"Host: myhost.com\" localhost:8080"
echo
