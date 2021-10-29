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

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

export KUADRANT_NAMESPACE="kuadrant-system"
export KIND_CLUSTER_NAME="kuadrant-local"

${SCRIPT_DIR}/deploy-kuadrant-deps.sh

echo "Building kuadrant"
docker build -t kuadrant:devel ./
kind load docker-image kuadrant:devel --name ${KIND_CLUSTER_NAME}
echo "Deploying Kuadrant control plane"
kustomize build config/default | kubectl -n "${KUADRANT_NAMESPACE}" apply -f -
kubectl -n "${KUADRANT_NAMESPACE}" patch deployment kuadrant-controller-manager -p '{"spec": {"template": {"spec":{"containers":[{"name": "manager","image":"kuadrant:devel", "env": [{"name": "LOG_LEVEL", "value": "debug"}, {"name": "LOG_MODE", "value": "development"}], "imagePullPolicy":"IfNotPresent"}]}}}}'
echo "Wait for all deployments to be up"
kubectl -n "${KUADRANT_NAMESPACE}" wait --timeout=300s --for=condition=Available deployments --all

echo
echo "Now you can export the kuadrant gateway by doing:"
echo "kubectl port-forward --namespace ${KUADRANT_NAMESPACE} deployment/kuadrant-gateway 8080:8080 8443:8443"
echo "after that, you can curl -H \"Host: myhost.com\" localhost:8080"
echo "-- Linux only -- Ingress gateway is exported using nodePort service in port 9080"
echo "curl -H \"Host: myhost.com\" localhost:9080"
echo
