#!/usr/bin/env bash

set -euo pipefail

# Smoke test for Kuadrant installation
# Verifies operators are running and can accept policy resources
#
# Usage:
#   ./utils/disconnected-openshift-install/smoke-test.sh [--cleanup]
#
# Options:
#   --cleanup   Remove test resources after completion

CLEANUP="${1:-}"

echo "=========================================="
echo "Kuadrant Installation Smoke Test"
echo "=========================================="
echo ""

FAILED=false
FAILED_TESTS=()

# Test 1: Verify all operators are installed and running
echo "==> Test 1: Operator Installation"
echo ""

EXPECTED_CSVS=(
    "kuadrant-operator"
    "authorino-operator"
    "limitador-operator"
    "dns-operator"
)

# Wait for CSVs to reach Succeeded state (up to 180s)
echo "  Waiting for CSVs to succeed (up to 180s)..."
TIMEOUT=180
ELAPSED=0
ALL_CSVS_READY=false
while [ $ELAPSED -lt $TIMEOUT ]; do
    ALL_READY=true
    for csv_prefix in "${EXPECTED_CSVS[@]}"; do
        CSV_NAME=$(oc get csv -n kuadrant-system -o name 2>/dev/null | grep "${csv_prefix}" | head -1 || true)
        if [ -z "$CSV_NAME" ]; then
            ALL_READY=false
            break
        fi
        CSV_PHASE=$(oc get ${CSV_NAME} -n kuadrant-system -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
        if [ "$CSV_PHASE" != "Succeeded" ]; then
            ALL_READY=false
            break
        fi
    done

    if [ "$ALL_READY" = true ]; then
        ALL_CSVS_READY=true
        echo "    ✓ All CSVs succeeded (${ELAPSED}s)"
        break
    fi

    sleep 10
    ELAPSED=$((ELAPSED + 10))
    if [ $((ELAPSED % 30)) -eq 0 ]; then
        echo "    Still waiting... (${ELAPSED}s)"
    fi
done

echo ""
echo "  Checking CSVs in kuadrant-system namespace..."
for csv_prefix in "${EXPECTED_CSVS[@]}"; do
    CSV_NAME=$(oc get csv -n kuadrant-system -o name 2>/dev/null | grep "${csv_prefix}" | head -1 || true)

    if [ -z "$CSV_NAME" ]; then
        echo "    ✗ ${csv_prefix} - NOT FOUND"
        FAILED=true
    else
        CSV_PHASE=$(oc get ${CSV_NAME} -n kuadrant-system -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
        if [ "$CSV_PHASE" = "Succeeded" ]; then
            echo "    ✓ ${csv_prefix} - ${CSV_PHASE}"
        else
            echo "    ✗ ${csv_prefix} - ${CSV_PHASE} (expected: Succeeded)"
            FAILED=true
        fi
    fi
done
echo ""

# Wait for pods to be ready (up to 120s)
echo "  Waiting for operator pods to be ready (up to 120s)..."
TIMEOUT=120
ELAPSED=0
ALL_PODS_READY=false
while [ $ELAPSED -lt $TIMEOUT ]; do
    PODS=$(oc get pods -n kuadrant-system --no-headers 2>/dev/null | wc -l || echo "0")
    if [ "$PODS" -gt 0 ]; then
        READY=$(oc get pods -n kuadrant-system -o json 2>/dev/null | jq -r '.items[] | select(.status.conditions[] | select(.type=="Ready" and .status=="True")) | .metadata.name' | wc -l || echo "0")

        if [ "$READY" -eq "$PODS" ]; then
            ALL_PODS_READY=true
            echo "    ✓ All pods ready (${ELAPSED}s)"
            break
        fi
    fi

    sleep 10
    ELAPSED=$((ELAPSED + 10))
    if [ $((ELAPSED % 30)) -eq 0 ]; then
        echo "    Still waiting... (${ELAPSED}s)"
    fi
done
echo ""

echo "  Checking operator pods..."
PODS=$(oc get pods -n kuadrant-system --no-headers 2>/dev/null | wc -l || echo "0")
if [ "$PODS" -gt 0 ]; then
    # Check pod readiness, not just Running phase
    READY=$(oc get pods -n kuadrant-system -o json 2>/dev/null | jq -r '.items[] | select(.status.conditions[] | select(.type=="Ready" and .status=="True")) | .metadata.name' | wc -l || echo "0")
    echo "    Pods ready: ${READY}/${PODS}"

    # Show detailed pod status with readiness
    echo "    Pod status:"
    oc get pods -n kuadrant-system -o custom-columns=NAME:.metadata.name,READY:.status.conditions[?\(@.type==\"Ready\"\)].status,STATUS:.status.phase,RESTARTS:.status.containerStatuses[0].restartCount 2>/dev/null | sed 's/^/      /'

    # Check for pods not ready
    if [ "$READY" -lt "$PODS" ]; then
        echo ""
        echo "    ✗ FAILED - Not all pods are ready after ${TIMEOUT}s"
        FAILED=true

        # Show logs from non-ready pods
        echo "    Checking logs from non-ready pods:"
        for pod in $(oc get pods -n kuadrant-system -o json | jq -r '.items[] | select(.status.conditions[] | select(.type=="Ready" and .status!="True")) | .metadata.name'); do
            echo "      Logs from ${pod}:"
            oc logs -n kuadrant-system "${pod}" --tail=20 2>&1 | sed 's/^/        /'
        done
    else
        echo "    ✓ All pods ready"
    fi

    # Check for high restart counts (indicates crashlooping)
    echo ""
    echo "  Checking for crashlooping pods..."
    HIGH_RESTARTS=$(oc get pods -n kuadrant-system -o json 2>/dev/null | jq -r '.items[] | select(.status.containerStatuses[0].restartCount > 3) | .metadata.name' || echo "")
    if [ -n "$HIGH_RESTARTS" ]; then
        echo "    ✗ FAILED - Pods with high restart counts (>3):"
        echo "$HIGH_RESTARTS" | sed 's/^/      /'
        FAILED=true
    else
        echo "    ✓ No crashlooping pods detected"
    fi
else
    echo "    ✗ No pods found in kuadrant-system namespace"
    FAILED=true
fi
echo ""

# Test 2: Verify Kuadrant CR and operand CRs
echo "==> Test 2: Kuadrant Control Plane"
echo ""

echo "  Checking Kuadrant CR..."
if oc get kuadrant kuadrant -n kuadrant-system &>/dev/null; then
    echo "    ✓ Kuadrant CR exists"

    # Check Kuadrant status
    KUADRANT_READY=$(oc get kuadrant kuadrant -n kuadrant-system -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "Unknown")
    if [ "$KUADRANT_READY" = "True" ]; then
        echo "      ✓ Kuadrant is ready"
    else
        echo "      ✗ FAILED - Kuadrant not ready (status: ${KUADRANT_READY})"
        echo "      Kuadrant CR must be ready for operator to function"
        oc get kuadrant kuadrant -n kuadrant-system -o jsonpath='{.status.conditions}' 2>/dev/null | jq '.' | sed 's/^/        /'
        FAILED=true
    fi
else
    echo "    ✗ FAILED - Kuadrant CR not found"
    echo "      Installation incomplete - run: ./test-disconnected/install/install.sh"
    FAILED=true
fi
echo ""

echo "  Checking Authorino CR (created by kuadrant-operator)..."
if oc get authorino authorino -n kuadrant-system &>/dev/null; then
    echo "    ✓ Authorino CR exists"

    # Check Authorino status
    AUTH_READY=$(oc get authorino authorino -n kuadrant-system -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "Unknown")
    if [ "$AUTH_READY" = "True" ]; then
        echo "      ✓ Authorino CR is ready"
    else
        echo "      ✗ FAILED - Authorino CR not ready (status: ${AUTH_READY})"
        FAILED=true
    fi
else
    echo "    ✗ FAILED - Authorino CR not found"
    echo "      kuadrant-operator should create Authorino CR when Kuadrant CR is created"
    echo "      Check kuadrant-operator logs for errors"
    FAILED=true
fi
echo ""

echo "  Checking Limitador CR (created by kuadrant-operator)..."
if oc get limitador limitador -n kuadrant-system &>/dev/null; then
    echo "    ✓ Limitador CR exists"

    # Check Limitador status
    LIM_READY=$(oc get limitador limitador -n kuadrant-system -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "Unknown")
    if [ "$LIM_READY" = "True" ]; then
        echo "      ✓ Limitador CR is ready"
    else
        echo "      ✗ FAILED - Limitador CR not ready (status: ${LIM_READY})"
        FAILED=true
    fi
else
    echo "    ✗ FAILED - Limitador CR not found"
    echo "      kuadrant-operator should create Limitador CR when Kuadrant CR is created"
    echo "      Check kuadrant-operator logs for errors"
    FAILED=true
fi
echo ""

# Test 3: Verify CRDs are installed
echo "==> Test 3: Custom Resource Definitions"
echo ""

EXPECTED_CRDS=(
    "authpolicies.kuadrant.io"
    "ratelimitpolicies.kuadrant.io"
    "dnspolicies.kuadrant.io"
    "tlspolicies.kuadrant.io"
    "authconfigs.authorino.kuadrant.io"
    "limitadors.limitador.kuadrant.io"
    "dnsrecords.kuadrant.io"
)

echo "  Checking CRDs..."
for crd in "${EXPECTED_CRDS[@]}"; do
    if oc get crd "${crd}" &>/dev/null; then
        echo "    ✓ ${crd}"
    else
        echo "    ✗ ${crd} - NOT FOUND"
        FAILED=true
    fi
done
echo ""

# Test 4: Istio Gateway API Provider
echo "==> Test 4: Istio Gateway API Provider"
echo ""

echo "  Checking for Istio installation..."
if ! oc get gatewayclass istio &>/dev/null; then
    echo "    ✗ FAILED - Istio GatewayClass not found"
    echo "      Istio must be installed for proper Kuadrant validation"
    echo "      Run: ./utils/disconnected-openshift-install/install-istio.sh"
    FAILED=true
else
    echo "    ✓ Istio GatewayClass found"

    # Check GatewayClass status
    GWC_ACCEPTED=$(oc get gatewayclass istio -o jsonpath='{.status.conditions[?(@.type=="Accepted")].status}' 2>/dev/null || echo "Unknown")
    if [ "$GWC_ACCEPTED" = "True" ]; then
        echo "      ✓ GatewayClass accepted"
    else
        echo "      ⚠ GatewayClass status: ${GWC_ACCEPTED}"
    fi
fi
echo ""

echo "  Checking istiod deployment..."
if oc get deployment istiod -n istio-system &>/dev/null; then
    ISTIOD_READY=$(oc get deployment istiod -n istio-system -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
    ISTIOD_READY="${ISTIOD_READY:-0}"
    if [ "$ISTIOD_READY" -gt 0 ]; then
        echo "    ✓ istiod is ready (${ISTIOD_READY} replicas)"
    else
        echo "    ✗ FAILED - istiod not ready"
        FAILED=true
    fi
else
    echo "    ✗ FAILED - istiod deployment not found"
    echo "      Istio control plane is not deployed"
    FAILED=true
fi
echo ""

# Test 5: Policy Reconciliation with Real Gateway
echo "==> Test 5: Policy Reconciliation with Real Gateway"
echo ""

TEST_NS="kuadrant-smoke-test"
echo "  Creating test namespace: ${TEST_NS}"
oc create namespace ${TEST_NS} --dry-run=client -o yaml | oc apply -f - 2>/dev/null
echo ""

# Create a simple backend service (using Kuadrant httpbin - mirrored by oc-mirror)
echo "  Creating backend deployment and service..."
cat <<YAML | oc apply -f - 2>/dev/null
apiVersion: apps/v1
kind: Deployment
metadata:
  name: smoke-test-backend
  namespace: ${TEST_NS}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: smoke-test-backend
  template:
    metadata:
      labels:
        app: smoke-test-backend
    spec:
      containers:
      - name: httpbin
        image: quay.io/kuadrant/httpbin:latest
        command:
        - gunicorn
        - -b
        - "0.0.0.0:8080"
        - "httpbin:app"
        - -k
        - gevent
        ports:
        - containerPort: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: smoke-test-backend
  namespace: ${TEST_NS}
spec:
  selector:
    app: smoke-test-backend
  ports:
  - port: 8080
    targetPort: 8080
YAML

if oc get deployment smoke-test-backend -n ${TEST_NS} &>/dev/null; then
    echo "    ✓ Backend deployment created"
    # Wait for backend to be ready
    echo "    Waiting for backend pod to be ready..."
    oc wait --for=condition=available --timeout=90s deployment/smoke-test-backend -n ${TEST_NS} 2>/dev/null || echo "    ⚠ Backend deployment not ready yet (may still be pulling image)"
else
    echo "    ✗ Backend deployment creation failed"
    FAILED=true
fi
echo ""

# Create Gateway using Istio GatewayClass
echo "  Creating Gateway (using istio GatewayClass)..."
cat <<YAML | oc apply -f - 2>/dev/null
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: smoke-test-gateway
  namespace: ${TEST_NS}
  annotations:
    networking.istio.io/service-type: NodePort
spec:
  gatewayClassName: istio
  listeners:
  - name: http
    protocol: HTTP
    port: 80
    hostname: "*.test.example.com"
YAML

if oc get gateway smoke-test-gateway -n ${TEST_NS} &>/dev/null; then
    echo "    ✓ Gateway created"

    # Wait for Istio to program the Gateway (NodePort should allow it to reach Programmed)
    echo "    Waiting for Gateway to be programmed by Istio (60s timeout)..."
    TIMEOUT=60
    ELAPSED=0
    while [ $ELAPSED -lt $TIMEOUT ]; do
        GW_PROGRAMMED=$(oc get gateway smoke-test-gateway -n ${TEST_NS} -o jsonpath='{.status.conditions[?(@.type=="Programmed")].status}' 2>/dev/null || echo "")
        GW_ACCEPTED=$(oc get gateway smoke-test-gateway -n ${TEST_NS} -o jsonpath='{.status.conditions[?(@.type=="Accepted")].status}' 2>/dev/null || echo "")

        if [ "$GW_PROGRAMMED" = "True" ]; then
            echo "      ✓ Gateway programmed by Istio (NodePort service ready)"
            break
        elif [ "$GW_ACCEPTED" = "True" ] && [ $ELAPSED -gt 30 ]; then
            # If accepted but not programmed after 30s, show status
            echo "      ⚠ Gateway accepted but not yet programmed (still waiting...)"
        fi
        sleep 3
        ELAPSED=$((ELAPSED + 3))
    done

    if [ "$GW_PROGRAMMED" != "True" ]; then
        echo "      ✗ FAILED - Gateway not programmed by Istio (status: ${GW_PROGRAMMED})"
        echo "      Gateway status:"
        oc get gateway smoke-test-gateway -n ${TEST_NS} -o jsonpath='{.status.conditions}' 2>/dev/null | jq '.' | sed 's/^/        /'
        FAILED=true
    fi
else
    echo "    ✗ Gateway creation failed"
    FAILED=true
fi
echo ""

# Create HTTPRoute
echo "  Creating HTTPRoute..."
cat <<YAML | oc apply -f - 2>/dev/null
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: smoke-test-route
  namespace: ${TEST_NS}
spec:
  parentRefs:
  - name: smoke-test-gateway
  hostnames:
  - "api.test.example.com"
  rules:
  - backendRefs:
    - name: smoke-test-backend
      port: 8080
YAML

if oc get httproute smoke-test-route -n ${TEST_NS} &>/dev/null; then
    echo "    ✓ HTTPRoute created"
else
    echo "    ✗ HTTPRoute creation failed"
    FAILED=true
fi
echo ""

# Create RateLimitPolicy targeting HTTPRoute
echo "  Creating RateLimitPolicy..."
cat <<YAML | oc apply -f - 2>/dev/null
apiVersion: kuadrant.io/v1
kind: RateLimitPolicy
metadata:
  name: smoke-test-rlp
  namespace: ${TEST_NS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: smoke-test-route
  limits:
    "smoke-test-limit":
      rates:
      - limit: 5
        window: 10s
YAML

if oc get ratelimitpolicy smoke-test-rlp -n ${TEST_NS} &>/dev/null; then
    echo "    ✓ RateLimitPolicy created"
else
    echo "    ✗ RateLimitPolicy creation failed"
    FAILED=true
fi
echo ""

# Create AuthPolicy targeting HTTPRoute
echo "  Creating AuthPolicy..."
cat <<YAML | oc apply -f - 2>/dev/null
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: smoke-test-authpolicy
  namespace: ${TEST_NS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: smoke-test-route
  rules:
    authentication:
      "anonymous":
        anonymous: {}
YAML

if oc get authpolicy smoke-test-authpolicy -n ${TEST_NS} &>/dev/null; then
    echo "    ✓ AuthPolicy created"
else
    echo "    ✗ AuthPolicy creation failed"
    FAILED=true
fi
echo ""

# Wait for Authorino to be ready first (required for AuthPolicy reconciliation)
echo "  Waiting for Authorino to be ready (90s timeout)..."
TIMEOUT=90
ELAPSED=0
AUTHORINO_READY=false
while [ $ELAPSED -lt $TIMEOUT ]; do
    AUTHORINO_REPLICAS=$(oc get deployment authorino -n kuadrant-system -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
    AUTHORINO_REPLICAS="${AUTHORINO_REPLICAS:-0}"

    if [ "$AUTHORINO_REPLICAS" -gt 0 ]; then
        echo "    ✓ Authorino ready (${AUTHORINO_REPLICAS} replicas)"
        AUTHORINO_READY=true
        break
    fi

    sleep 5
    ELAPSED=$((ELAPSED + 5))
    if [ $((ELAPSED % 15)) -eq 0 ]; then
        echo "    Still waiting... (${ELAPSED}s)"
    fi
done

if [ "$AUTHORINO_READY" = false ]; then
    echo "    ⚠ Authorino not ready after ${TIMEOUT}s (continuing anyway)"
fi
echo ""

# Wait for policy reconciliation
echo "  Waiting for policy reconciliation (30 seconds)..."
sleep 30
echo ""

# Check policy status conditions
echo "  Checking policy status conditions..."
RLP_ACCEPTED=$(oc get ratelimitpolicy smoke-test-rlp -n ${TEST_NS} -o jsonpath='{.status.conditions[?(@.type=="Accepted")].status}' 2>/dev/null || echo "Unknown")
if [ "$RLP_ACCEPTED" = "True" ]; then
    echo "    ✓ RateLimitPolicy accepted"
else
    echo "    ✗ FAILED - RateLimitPolicy not accepted (status: ${RLP_ACCEPTED})"
    echo "      Policy may not be reconciling correctly"
    oc get ratelimitpolicy smoke-test-rlp -n ${TEST_NS} -o jsonpath='{.status.conditions}' 2>/dev/null | jq '.' | sed 's/^/      /'
    FAILED=true
fi

AUTH_ACCEPTED=$(oc get authpolicy smoke-test-authpolicy -n ${TEST_NS} -o jsonpath='{.status.conditions[?(@.type=="Accepted")].status}' 2>/dev/null || echo "Unknown")
if [ "$AUTH_ACCEPTED" = "True" ]; then
    echo "    ✓ AuthPolicy accepted"
else
    echo "    ✗ FAILED - AuthPolicy not accepted (status: ${AUTH_ACCEPTED})"
    echo "      Policy may not be reconciling correctly"
    oc get authpolicy smoke-test-authpolicy -n ${TEST_NS} -o jsonpath='{.status.conditions}' 2>/dev/null | jq '.' | sed 's/^/      /'
    FAILED=true
fi
echo ""

# Test actual traffic through the Gateway
echo "  Testing traffic through Gateway..."
# Istio creates a service with pattern {gateway-name}-istio
GW_SERVICE="smoke-test-gateway-istio"
if oc get service ${GW_SERVICE} -n ${TEST_NS} &>/dev/null; then
    echo "    Gateway service: ${GW_SERVICE}"

    # Test using cluster-internal DNS (from a test pod)
    echo "    Testing via cluster-internal request..."

    # Create a test pod to make internal requests (using mirrored httpbin image with python)
    if ! oc get pod test-client -n ${TEST_NS} &>/dev/null; then
        cat <<YAML | oc apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: test-client
  namespace: ${TEST_NS}
spec:
  containers:
  - name: client
    image: quay.io/kuadrant/httpbin:latest
    command: ["/bin/sh", "-c", "sleep 3600"]
YAML
        # Wait for test client pod to be ready
        oc wait --for=condition=ready --timeout=30s pod/test-client -n ${TEST_NS} 2>/dev/null || echo "    ⚠ Test client pod not ready yet"
    fi

    # Test 1: Basic routing through Gateway
    echo "    Testing basic routing..."
    RESPONSE=$(oc exec -n ${TEST_NS} test-client -- python3 -c "import urllib.request; print(urllib.request.urlopen(urllib.request.Request('http://${GW_SERVICE}.${TEST_NS}.svc.cluster.local:80/', headers={'Host': 'api.test.example.com'})).status)" 2>/dev/null || echo "000")

    if [ "$RESPONSE" = "200" ]; then
        echo "      ✓ Traffic flows through Gateway (HTTP ${RESPONSE})"
    elif [ "$RESPONSE" = "000" ]; then
        echo "      ✗ FAILED - Could not reach Gateway service (connection failed)"
        echo "        Envoy proxies may not be ready"
        FAILED=true
    else
        echo "      ⚠ Unexpected response: HTTP ${RESPONSE}"
    fi

    # Test 2: Verify rate limiting enforcement
    if [ "$RESPONSE" = "200" ]; then
        echo "    Testing rate limiting (5 requests/10s limit)..."
        # Make 6 requests rapidly to trigger rate limit
        SUCCESS_COUNT=0
        RATE_LIMITED=false
        for i in {1..6}; do
            CODE=$(oc exec -n ${TEST_NS} test-client -- python3 -c "
try:
    import urllib.request
    print(urllib.request.urlopen(urllib.request.Request('http://${GW_SERVICE}.${TEST_NS}.svc.cluster.local:80/', headers={'Host': 'api.test.example.com'})).status)
except urllib.error.HTTPError as e:
    print(e.code)
" 2>/dev/null || echo "000")
            if [ "$CODE" = "200" ]; then
                SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
            elif [ "$CODE" = "429" ]; then
                RATE_LIMITED=true
                break
            fi
            sleep 0.2
        done

        if [ "$RATE_LIMITED" = true ]; then
            echo "      ✓ Rate limiting enforced (HTTP 429 after ${SUCCESS_COUNT} requests)"
            # Wait for rate limit window to expire before next test
            echo "      Waiting 10s for rate limit window to reset..."
            sleep 10
        else
            echo "      ⚠ Rate limiting not triggered (all 6 requests succeeded)"
            echo "        This may indicate WASM filter is not loading or rate limit policy not enforced"
            echo "        Check Gateway proxy logs: oc logs -n ${TEST_NS} -l istio.io/gateway-name=smoke-test-gateway"
        fi
    fi

    # Test 3: Verify auth is configured (anonymous access allowed)
    echo "    Testing authentication policy..."
    # Send request without any auth headers - should succeed with anonymous policy
    AUTH_RESPONSE=$(oc exec -n ${TEST_NS} test-client -- python3 -c "
try:
    import urllib.request
    print(urllib.request.urlopen(urllib.request.Request('http://${GW_SERVICE}.${TEST_NS}.svc.cluster.local:80/', headers={'Host': 'api.test.example.com'})).status)
except urllib.error.HTTPError as e:
    print(e.code)
" 2>/dev/null || echo "000")

    if [ "$AUTH_RESPONSE" = "200" ]; then
        echo "      ✓ Anonymous authentication allows access (HTTP ${AUTH_RESPONSE})"
    elif [ "$AUTH_RESPONSE" = "401" ] || [ "$AUTH_RESPONSE" = "403" ]; then
        echo "      ✗ FAILED - Authentication blocked request (HTTP ${AUTH_RESPONSE})"
        echo "        Anonymous policy should allow access"
        FAILED=true
    else
        echo "      ⚠ Unexpected auth response: HTTP ${AUTH_RESPONSE}"
    fi

    # Test 4: Create stricter auth policy and verify it blocks requests
    echo "    Testing authentication enforcement (API key required)..."
    cat <<YAML | oc apply -f - 2>/dev/null
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: smoke-test-authpolicy-strict
  namespace: ${TEST_NS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: smoke-test-route
  rules:
    authentication:
      "api-key":
        apiKey:
          selector:
            matchLabels:
              app: toystore
        credentials:
          authorizationHeader:
            prefix: APIKEY
YAML

    # Wait for policy to reconcile
    sleep 5

    # Try to access without API key - should be denied
    STRICT_AUTH_RESPONSE=$(oc exec -n ${TEST_NS} test-client -- python3 -c "
try:
    import urllib.request
    print(urllib.request.urlopen(urllib.request.Request('http://${GW_SERVICE}.${TEST_NS}.svc.cluster.local:80/', headers={'Host': 'api.test.example.com'})).status)
except urllib.error.HTTPError as e:
    print(e.code)
" 2>/dev/null || echo "000")

    if [ "$STRICT_AUTH_RESPONSE" = "401" ] || [ "$STRICT_AUTH_RESPONSE" = "403" ]; then
        echo "      ✓ Authentication blocks unauthorized requests (HTTP ${STRICT_AUTH_RESPONSE})"
    elif [ "$STRICT_AUTH_RESPONSE" = "200" ]; then
        echo "      ⚠ Authentication did not block request (HTTP ${STRICT_AUTH_RESPONSE})"
        echo "        This may indicate auth filter is not enforcing policies"
    else
        echo "      ⚠ Unexpected response: HTTP ${STRICT_AUTH_RESPONSE}"
    fi

    # Restore anonymous policy for cleanup
    oc delete authpolicy smoke-test-authpolicy-strict -n ${TEST_NS} 2>/dev/null || true
    sleep 2
else
    echo "    ⚠ Gateway service not found yet"
    echo "      Service should be created by Istio - check 'oc get svc -n ${TEST_NS}'"
fi
echo ""

# Verify AuthConfig was created by AuthPolicy (proves auth policy reconciliation works)
# Note: With Istio integration, AuthConfigs are created in kuadrant-system namespace
echo "  Verifying AuthConfig creation (from AuthPolicy)..."
AUTH_CONFIG_COUNT=$(oc get authconfig -n kuadrant-system --no-headers 2>/dev/null | wc -l || echo "0")
if [ "$AUTH_CONFIG_COUNT" -gt 0 ]; then
    echo "    ✓ AuthConfig created (${AUTH_CONFIG_COUNT} resource(s) in kuadrant-system)"
    oc get authconfig -n kuadrant-system --no-headers 2>/dev/null | awk '{print "      - " $1}'

    # Check AuthConfig status
    for authconfig in $(oc get authconfig -n kuadrant-system -o name 2>/dev/null); do
        AUTH_READY=$(oc get ${authconfig} -n kuadrant-system -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "Unknown")
        AUTH_NAME=$(echo ${authconfig} | cut -d'/' -f2)
        if [ "$AUTH_READY" = "True" ]; then
            echo "      ✓ ${AUTH_NAME} is ready"
        else
            echo "      ⚠ ${AUTH_NAME} status: ${AUTH_READY}"
        fi
    done
else
    echo "    ✗ FAILED - No AuthConfig resources found in kuadrant-system"
    echo "      This indicates kuadrant-operator is not reconciling AuthPolicy correctly"
    echo "      Check operator logs: oc logs -n kuadrant-system deployment/kuadrant-operator-controller-manager"
    FAILED=true
fi
echo ""

# Verify EnvoyFilters were created (Istio integration for policies)
echo "  Verifying EnvoyFilter creation (Istio integration)..."
ENVOYFILTER_COUNT=$(oc get envoyfilter -n ${TEST_NS} --no-headers 2>/dev/null | wc -l || echo "0")
if [ "$ENVOYFILTER_COUNT" -gt 0 ]; then
    echo "    ✓ EnvoyFilters created (${ENVOYFILTER_COUNT} resource(s) in ${TEST_NS})"
    oc get envoyfilter -n ${TEST_NS} --no-headers 2>/dev/null | awk '{print "      - " $1}'

    # Check for expected EnvoyFilters
    EXPECTED_FILTERS=("kuadrant-ratelimiting-smoke-test-gateway" "kuadrant-auth-smoke-test-gateway" "kuadrant-smoke-test-gateway")
    for filter in "${EXPECTED_FILTERS[@]}"; do
        if oc get envoyfilter "$filter" -n ${TEST_NS} &>/dev/null; then
            echo "      ✓ ${filter} exists"
        else
            echo "      ⚠ ${filter} not found (may indicate policy reconciliation issue)"
        fi
    done

    # Validate wasm filter configuration
    echo "    Checking WASM filter configuration..."
    WASM_FILTER=$(oc get envoyfilter kuadrant-smoke-test-gateway -n ${TEST_NS} -o jsonpath='{.spec.configPatches[?(@.patch.value.name=="envoy.filters.http.wasm")]}' 2>/dev/null)
    if [ -n "$WASM_FILTER" ]; then
        echo "      ✓ WASM filter configured in kuadrant-smoke-test-gateway"

        # Check wasm module source
        WASM_URI=$(oc get envoyfilter kuadrant-smoke-test-gateway -n ${TEST_NS} -o jsonpath='{.spec.configPatches[?(@.patch.value.name=="envoy.filters.http.wasm")].patch.value.typed_config.value.config.vm_config.code.remote.http_uri.uri}' 2>/dev/null)
        if [ -n "$WASM_URI" ]; then
            echo "      WASM module URI: ${WASM_URI}"

            # Check if wasm service endpoint exists
            if oc get service kuadrant-operator-wasm -n kuadrant-system &>/dev/null; then
                echo "      ✓ kuadrant-operator-wasm service exists (serves wasm module)"
            else
                echo "      ✗ FAILED - kuadrant-operator-wasm service not found"
                echo "        WASM module cannot be loaded without this service"
                FAILED=true
            fi
        fi
    else
        echo "      ✗ FAILED - WASM filter not configured in EnvoyFilter"
        echo "        Policy enforcement via WASM will not work"
        FAILED=true
    fi
else
    echo "    ✗ FAILED - No EnvoyFilter resources found in ${TEST_NS}"
    echo "      EnvoyFilters are required for Istio integration with Kuadrant policies"
    echo "      Check that Gateway is Programmed and policies are Accepted"
    FAILED=true
fi
echo ""

# Test 6: Check if Authorino is deployed (authorino-operator should deploy it)
echo "==> Test 6: Authorino Deployment"
echo ""

if oc get deployment authorino -n kuadrant-system &>/dev/null; then
    AUTHORINO_REPLICAS=$(oc get deployment authorino -n kuadrant-system -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
    # Handle empty string from jsonpath when field doesn't exist
    AUTHORINO_REPLICAS="${AUTHORINO_REPLICAS:-0}"
    if [ "$AUTHORINO_REPLICAS" -gt 0 ]; then
        echo "  ✓ Authorino is deployed and ready (${AUTHORINO_REPLICAS} replicas)"
    else
        echo "  ⚠ Authorino is deployed but not ready yet"
    fi

    # Show image reference
    AUTHORINO_IMAGE=$(oc get deployment authorino -n kuadrant-system -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null)
    if [ -n "$AUTHORINO_IMAGE" ]; then
        echo "  Image: ${AUTHORINO_IMAGE}"
        # Check if using digest reference (informational)
        if echo "$AUTHORINO_IMAGE" | grep -q "@sha256:"; then
            echo "    Using digest reference (immutable)"
        else
            echo "    Using tag reference"
        fi
    fi
else
    echo "  ✗ FAILED - Authorino deployment not found"
    echo "      authorino-operator should create Authorino deployment from Authorino CR"
    echo "      Check authorino-operator logs for errors"
    FAILED=true
fi
echo ""

# Test 7: Check if Limitador is deployed
echo "==> Test 7: Limitador Deployment"
echo ""

if oc get deployment limitador-limitador -n kuadrant-system &>/dev/null; then
    LIMITADOR_REPLICAS=$(oc get deployment limitador-limitador -n kuadrant-system -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
    # Handle empty string from jsonpath when field doesn't exist
    LIMITADOR_REPLICAS="${LIMITADOR_REPLICAS:-0}"
    if [ "$LIMITADOR_REPLICAS" -gt 0 ]; then
        echo "  ✓ Limitador is deployed and ready (${LIMITADOR_REPLICAS} replicas)"
    else
        echo "  ⚠ Limitador is deployed but not ready yet"
    fi

    # Show image reference
    LIMITADOR_IMAGE=$(oc get deployment limitador-limitador -n kuadrant-system -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null)
    if [ -n "$LIMITADOR_IMAGE" ]; then
        echo "  Image: ${LIMITADOR_IMAGE}"
        # Check if using digest reference (informational)
        if echo "$LIMITADOR_IMAGE" | grep -q "@sha256:"; then
            echo "    Using digest reference (immutable)"
        else
            echo "    Using tag reference"
        fi
    fi
else
    echo "  ✗ FAILED - Limitador deployment not found"
    echo "      limitador-operator should create Limitador deployment from Limitador CR"
    echo "      Check limitador-operator logs for errors"
    FAILED=true
fi
echo ""

# Test 8: Kuadrant Component Image Information
echo "==> Test 8: Kuadrant Component Images"
echo ""

OPERATOR_DEPLOYMENTS=(
    "kuadrant-operator-controller-manager"
    "authorino-operator"
    "limitador-operator-controller-manager"
    "dns-operator-controller-manager"
)

echo "  Component images in use:"
echo ""

# Operators
echo "  Operators:"
for deployment in "${OPERATOR_DEPLOYMENTS[@]}"; do
    if oc get deployment "${deployment}" -n kuadrant-system &>/dev/null; then
        OPERATOR_IMAGE=$(oc get deployment "${deployment}" -n kuadrant-system -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null)
        if [ -n "$OPERATOR_IMAGE" ]; then
            # Check if using digest reference (informational)
            if echo "$OPERATOR_IMAGE" | grep -q "@sha256:"; then
                IMAGE_TYPE="digest"
            else
                IMAGE_TYPE="tag"
            fi
            echo "    ${deployment}"
            echo "      Image: ${OPERATOR_IMAGE}"
            echo "      Type:  ${IMAGE_TYPE}"
            echo ""
        fi
    else
        echo "    ${deployment} - NOT FOUND"
        FAILED=true
    fi
done

# Operands
echo "  Operands:"
if oc get deployment authorino -n kuadrant-system &>/dev/null; then
    AUTH_IMAGE=$(oc get deployment authorino -n kuadrant-system -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null)
    if [ -n "$AUTH_IMAGE" ]; then
        if echo "$AUTH_IMAGE" | grep -q "@sha256:"; then
            AUTH_TYPE="digest"
        else
            AUTH_TYPE="tag"
        fi
        echo "    Authorino:"
        echo "      Image: ${AUTH_IMAGE}"
        echo "      Type:  ${AUTH_TYPE}"
        echo ""
    fi
fi

if oc get deployment limitador-limitador -n kuadrant-system &>/dev/null; then
    LIM_IMAGE=$(oc get deployment limitador-limitador -n kuadrant-system -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null)
    if [ -n "$LIM_IMAGE" ]; then
        if echo "$LIM_IMAGE" | grep -q "@sha256:"; then
            LIM_TYPE="digest"
        else
            LIM_TYPE="tag"
        fi
        echo "    Limitador:"
        echo "      Image: ${LIM_IMAGE}"
        echo "      Type:  ${LIM_TYPE}"
        echo ""
    fi
fi

# WASM Module (check EnvoyFilter for WASM URI)
if oc get envoyfilter -A &>/dev/null 2>&1; then
    WASM_URI=$(oc get envoyfilter -A -o jsonpath='{.items[*].spec.configPatches[?(@.patch.value.name=="envoy.filters.http.wasm")].patch.value.typed_config.value.config.vm_config.code.remote.http_uri.uri}' 2>/dev/null | tr ' ' '\n' | head -1)
    if [ -n "$WASM_URI" ]; then
        echo "  WASM Module:"
        echo "    URI: ${WASM_URI}"
        echo ""
    fi
fi

echo "  Note: This is informational output showing what images are currently deployed."
echo "  Both tag-based and digest-based references can work in disconnected environments"
echo "  if properly mirrored via ImageContentSourcePolicy or ImageDigestMirrorSet."
echo ""

# Test 9: Check operator logs for errors
echo "==> Test 9: Operator Health Check"
echo ""

echo "  Checking kuadrant-operator logs for errors..."
OPERATOR_ERRORS=$(oc logs -n kuadrant-system deployment/kuadrant-operator-controller-manager --tail=100 2>/dev/null | grep -i "error\|fatal\|panic" | grep -v "errorSource" | head -10 || echo "")
if [ -n "$OPERATOR_ERRORS" ]; then
    echo "    ⚠ WARNING - Errors found in kuadrant-operator logs:"
    echo "$OPERATOR_ERRORS" | sed 's/^/      /'
    echo ""
    echo "    Full recent logs:"
    oc logs -n kuadrant-system deployment/kuadrant-operator-controller-manager --tail=50 2>/dev/null | sed 's/^/      /'
    echo ""
    # Don't fail on log errors as they might be transient, but surface them
else
    echo "    ✓ No critical errors in recent logs"
fi
echo ""

# Cleanup
if [ "$CLEANUP" = "--cleanup" ]; then
    echo "==> Cleaning up test resources"
    echo ""
    oc delete namespace ${TEST_NS} 2>/dev/null || true
    echo "  ✓ Test namespace deleted"
    echo ""
fi

# Summary
echo "=========================================="
if [ "$FAILED" = false ]; then
    echo "✓ Smoke Test PASSED"
    echo "=========================================="
    echo ""
    echo "  ✓ All operators installed and running"
    echo "  ✓ Kuadrant control plane ready"
    echo "  ✓ Policies reconcile correctly"
    echo ""

    exit 0
else
    echo "✗ Smoke Test FAILED"
    echo "=========================================="
    echo ""

    # Show which tests failed
    echo "Failed checks:"
    echo ""

    # Parse output to identify which tests had failures
    # We'll collect the most common failure reasons
    FAILURE_SUMMARY=""

    # Check for specific failures
    if ! oc get gatewayclass istio &>/dev/null 2>&1; then
        echo "  ✗ Istio Gateway API Provider not found"
        FAILURE_SUMMARY="${FAILURE_SUMMARY}istio,"
    fi

    if ! oc get deployment istiod -n istio-system &>/dev/null 2>&1; then
        echo "  ✗ Istio control plane (istiod) not deployed"
        FAILURE_SUMMARY="${FAILURE_SUMMARY}istio,"
    fi

    if ! oc get kuadrant kuadrant -n kuadrant-system &>/dev/null 2>&1; then
        echo "  ✗ Kuadrant CR not created"
        FAILURE_SUMMARY="${FAILURE_SUMMARY}kuadrant-cr,"
    fi

    KUADRANT_READY=$(oc get kuadrant kuadrant -n kuadrant-system -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "Unknown")
    if [ "$KUADRANT_READY" != "True" ]; then
        echo "  ✗ Kuadrant control plane not ready (status: ${KUADRANT_READY})"
        FAILURE_SUMMARY="${FAILURE_SUMMARY}kuadrant-ready,"
    fi

    AUTHORINO_REPLICAS=$(oc get deployment authorino -n kuadrant-system -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
    AUTHORINO_REPLICAS="${AUTHORINO_REPLICAS:-0}"
    if [ "$AUTHORINO_REPLICAS" -eq 0 ]; then
        echo "  ✗ Authorino not ready (may need more time to start)"
        FAILURE_SUMMARY="${FAILURE_SUMMARY}authorino,"
    fi

    LIMITADOR_REPLICAS=$(oc get deployment limitador-limitador -n kuadrant-system -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
    LIMITADOR_REPLICAS="${LIMITADOR_REPLICAS:-0}"
    if [ "$LIMITADOR_REPLICAS" -eq 0 ]; then
        echo "  ✗ Limitador not ready (may need more time to start)"
        FAILURE_SUMMARY="${FAILURE_SUMMARY}limitador,"
    fi

    RLP_ACCEPTED=$(oc get ratelimitpolicy smoke-test-rlp -n kuadrant-smoke-test -o jsonpath='{.status.conditions[?(@.type=="Accepted")].status}' 2>/dev/null || echo "Unknown")
    if [ "$RLP_ACCEPTED" != "True" ] && [ "$RLP_ACCEPTED" != "Unknown" ]; then
        echo "  ✗ RateLimitPolicy not accepted (status: ${RLP_ACCEPTED})"
        FAILURE_SUMMARY="${FAILURE_SUMMARY}ratelimitpolicy,"
    fi

    AUTH_ACCEPTED=$(oc get authpolicy smoke-test-authpolicy -n kuadrant-smoke-test -o jsonpath='{.status.conditions[?(@.type=="Accepted")].status}' 2>/dev/null || echo "Unknown")
    if [ "$AUTH_ACCEPTED" != "True" ] && [ "$AUTH_ACCEPTED" != "Unknown" ]; then
        echo "  ✗ AuthPolicy not accepted (status: ${AUTH_ACCEPTED})"
        FAILURE_SUMMARY="${FAILURE_SUMMARY}authpolicy,"
    fi

    echo ""

    exit 1
fi
