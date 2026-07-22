#!/usr/bin/env bash
# Smoke test for toystore AuthPolicy and RateLimitPolicy on CRC.
#
# Usage:
#   ./hack/test-toystore.sh          # Run all tests
#   ./hack/test-toystore.sh auth     # Run only auth tests
#   ./hack/test-toystore.sh rate     # Run only rate limit tests

set -euo pipefail

TOYSTORE_URL="${TOYSTORE_URL:-https://toystore.apps-crc.testing}"
TOYSTORE_NAMESPACE="${TOYSTORE_NAMESPACE:-toystore}"
RLP_NAME="${RLP_NAME:-toystore-httproute}"
CURL="curl -sk --connect-timeout 5 -o /dev/null -w %{http_code}"

PASS=0
FAIL=0

pass() { echo "  PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  FAIL: $1 (expected $2, got $3)"; FAIL=$((FAIL + 1)); }

assert_status() {
    local desc="$1" expected="$2" actual="$3"
    if [[ "$actual" == "$expected" ]]; then
        pass "$desc"
    else
        fail "$desc" "$expected" "$actual"
    fi
}

request() {
    local method="${1:-GET}" path="${2:-/toy}" key="${3:-}"
    local args=(-X "$method")
    [[ -n "$key" ]] && args+=(-H "Authorization: APIKEY $key")
    $CURL "${args[@]}" "${TOYSTORE_URL}${path}"
}

# ── Read rate limits from cluster ──────────────────────────────────

read_limits() {
    local rlp
    rlp=$(kubectl get ratelimitpolicy "$RLP_NAME" -n "$TOYSTORE_NAMESPACE" -o json 2>/dev/null) || {
        echo "[WARN] Could not read RateLimitPolicy ${RLP_NAME} in ${TOYSTORE_NAMESPACE}, using defaults"
        GET_TOY_LIMIT=5
        GET_TOY_WINDOW="1m"
        GLOBAL_LIMIT=6
        GLOBAL_WINDOW="30s"
        return
    }

    GET_TOY_LIMIT=$(echo "$rlp" | jq -r '.spec.limits["get-toy"].rates[0].limit // 5')
    GET_TOY_WINDOW=$(echo "$rlp" | jq -r '.spec.limits["get-toy"].rates[0].window // "1m"')
    GLOBAL_LIMIT=$(echo "$rlp" | jq -r '.spec.limits["global"].rates[0].limit // 6')
    GLOBAL_WINDOW=$(echo "$rlp" | jq -r '.spec.limits["global"].rates[0].window // "30s"')
}

window_seconds() {
    local w="$1"
    if [[ "$w" =~ ^([0-9]+)s$ ]]; then
        echo "${BASH_REMATCH[1]}"
    elif [[ "$w" =~ ^([0-9]+)m$ ]]; then
        echo $((BASH_REMATCH[1] * 60))
    else
        echo "60"
    fi
}

# ── Auth Tests ──────────────────────────────────────────────────────

test_auth() {
    echo ""
    echo "=== Authentication Tests ==="
    echo ""

    echo "-- Unauthenticated requests should be rejected --"
    assert_status "GET /toy without key returns 401" \
        "401" "$(request GET /toy)"
    assert_status "POST /admin/toy without key returns 401" \
        "401" "$(request POST /admin/toy)"
    assert_status "DELETE /admin/toy without key returns 401" \
        "401" "$(request DELETE /admin/toy)"

    echo ""
    echo "-- Invalid key should be rejected --"
    assert_status "GET /toy with wrong key returns 401" \
        "401" "$(request GET /toy INVALIDKEY)"

    echo ""
    echo "-- Valid keys should be accepted --"
    assert_status "GET /toy with Alice key returns 200" \
        "200" "$(request GET /toy ALICEKEYFORDEMO)"
    assert_status "GET /toy with Bob key returns 200" \
        "200" "$(request GET /toy BOBKEYFORDEMO)"
    assert_status "GET /toy with Admin key returns 200" \
        "200" "$(request GET /toy IAMADMIN)"
}

# ── Rate Limit Tests ────────────────────────────────────────────────

wait_for_window() {
    local seconds="$1"
    echo ""
    echo "  (waiting ${seconds}s for rate limit window to reset...)"
    sleep "$seconds"
}

test_rate_limit() {
    read_limits

    # The effective limit for GET /toy is min(get-toy, global)
    local effective_limit=$GET_TOY_LIMIT
    if [[ "$GLOBAL_LIMIT" -lt "$effective_limit" ]]; then
        effective_limit=$GLOBAL_LIMIT
    fi

    # Send enough requests to exceed the limit
    local total_requests=$((effective_limit + 2))

    # Wait window is the longer of the two applicable windows
    local get_toy_secs global_secs wait_secs
    get_toy_secs=$(window_seconds "$GET_TOY_WINDOW")
    global_secs=$(window_seconds "$GLOBAL_WINDOW")
    wait_secs=$get_toy_secs
    if [[ "$global_secs" -gt "$wait_secs" ]]; then
        wait_secs=$global_secs
    fi

    echo ""
    echo "=== Rate Limit Tests ==="
    echo ""
    echo "  Cluster config: get-toy=${GET_TOY_LIMIT}/${GET_TOY_WINDOW}, global=${GLOBAL_LIMIT}/${GLOBAL_WINDOW}"
    echo "  Effective limit for GET /toy: ${effective_limit} (sending ${total_requests} requests)"

    echo ""
    echo "-- GET /toy rate limit: ${effective_limit} requests --"
    wait_for_window $((wait_secs + 2))

    local codes=()
    for i in $(seq 1 "$total_requests"); do
        codes+=("$(request GET /toy ALICEKEYFORDEMO)")
    done

    local ok=0 limited=0
    for c in "${codes[@]}"; do
        [[ "$c" == "200" ]] && ok=$((ok + 1))
        [[ "$c" == "429" ]] && limited=$((limited + 1))
    done

    echo "  Results: ${ok} x 200, ${limited} x 429 (out of ${total_requests} requests)"

    # Allow ±1 tolerance on the limit boundary
    local min_ok=$((effective_limit - 1))
    local max_ok=$((effective_limit + 1))
    if [[ "$min_ok" -lt 1 ]]; then min_ok=1; fi

    if [[ "$ok" -ge "$min_ok" && "$ok" -le "$max_ok" && "$limited" -ge 1 ]]; then
        pass "Rate limiting engaged around ${effective_limit} requests (got ${ok} ok)"
    else
        fail "Rate limiting" "${min_ok}-${max_ok} ok, 1+ limited" "${ok} ok, ${limited} limited"
    fi
}

# ── Main ────────────────────────────────────────────────────────────

SUITE="${1:-all}"

echo "Toystore Policy Smoke Tests"
echo "URL: $TOYSTORE_URL"

case "$SUITE" in
    auth)     test_auth ;;
    rate)     test_rate_limit ;;
    all)      test_auth; test_rate_limit ;;
    *)        echo "Usage: $0 [auth|rate|all]"; exit 1 ;;
esac

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Results: $PASS passed, $FAIL failed"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

[[ "$FAIL" -eq 0 ]]
