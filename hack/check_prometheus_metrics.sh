#!/bin/bash

# This script checks a list of PromQL queries against a Prometheus endpoint.
# For each query, it ensures that there is at least one result returned.
# It outputs the progress, number of results, and highlights any failures.

# If the prometheus API endpoint is a thanos querier instance in an openshift
# cluster, you can use port forwarding to access it.
# First, port forward to the thanos-querier service:
#
#   oc -n openshift-monitoring port-forward svc/thanos-querier 9090:9091
# 
# Then, in a new terminal, add permissions to your user:
#
#  oc adm policy add-cluster-role-to-user cluster-monitoring-view $(oc whoami)
#
# Get a TOKEN:
#
#  TOKEN=$(oc whoami -t)
#
# And use the script:
#
#  ./hack/check_prometheus_metrics.sh https://localhost:9090 $TOKEN
#

# Usage check
if [ $# -lt 1 ]; then
    echo "Usage: $0 <prometheus_endpoint> [oauth_token]"
    exit 1
fi

PROM_ENDPOINT=$1
TOKEN=$2

# Ensure 'jq' is installed for JSON parsing
if ! command -v jq &> /dev/null; then
    echo "Error: 'jq' is required but not installed."
    exit 1
fi

# Define the list of PromQL queries to check
QUERIES=(
    'count(istio_requests_total)'
    'up{job="kube-state-metrics"}'
    'count(container_cpu_usage_seconds_total)'
    'count(container_memory_working_set_bytes)'
    'count(container_network_receive_bytes_total)'
    'count(gatewayapi_httproute_labels)'
    'count(controller_runtime_reconcile_total)' #TODO: filter by job or namespace
    'count(controller_runtime_reconcile_errors_total)' #TODO: filter by job or namespace
    'count(workqueue_queue_duration_seconds_bucket)' #TODO: filter by job or namespace
    'count(process_resident_memory_bytes)' #TODO: filter by job or namespace
    'count(node_namespace_pod_container:container_cpu_usage_seconds_total:sum_irate)'
    # Add more queries here
)

TOTAL_QUERIES=${#QUERIES[@]}
CURRENT_QUERY=0

# ANSI color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Emojis
CHECK_MARK='\xE2\x9C\x94' # ✔
CROSS_MARK='\xE2\x9D\x8C' # ❌
WARNING_SIGN='\xE2\x9A\xA0' # ⚠️

# Set up the Authorization header if a token is provided
if [ -n "$TOKEN" ]; then
    AUTH_HEADER="Authorization: Bearer $TOKEN"
else
    AUTH_HEADER=""
fi

# Iterate over each query
for QUERY in "${QUERIES[@]}"; do
    CURRENT_QUERY=$((CURRENT_QUERY + 1))
    # URL-encode the query
    ENCODED_QUERY=$(echo -n "$QUERY" | jq -sRr @uri)

    # Make the API request to Prometheus using HTTPS and ignore certificate verification
    RESPONSE=$(curl -sk -H "$AUTH_HEADER" "${PROM_ENDPOINT}/api/v1/query?query=${ENCODED_QUERY}")

    # Check if the response is valid JSON
    if ! echo "$RESPONSE" | jq -e . >/dev/null 2>&1; then
        echo -e "${YELLOW}${CURRENT_QUERY}/${TOTAL_QUERIES} ${WARNING_SIGN} Failed to parse JSON response for '${QUERY}'${NC}"
        echo "Response was:"
        echo "$RESPONSE"
        continue
    fi

    # Extract the number of results
    RESULT_COUNT=$(echo "$RESPONSE" | jq '.data.result | length')

    if [ "$RESULT_COUNT" -gt 0 ] 2>/dev/null; then
        # Success output
        echo -e "${GREEN}${CURRENT_QUERY}/${TOTAL_QUERIES} ${CHECK_MARK} Found ${RESULT_COUNT} results for '${QUERY}'${NC}"
    else
        # Failure output
        echo -e "${RED}${CURRENT_QUERY}/${TOTAL_QUERIES} ${CROSS_MARK} No results for '${QUERY}'${NC}"
    fi
done
