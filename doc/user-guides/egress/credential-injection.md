# Egress Gateway Credential Injection

This guide demonstrates how to use AuthPolicy with an Istio egress gateway to inject credentials into requests to external services using [HashiCorp Vault](https://developer.hashicorp.com/vault) as the credential source.

## Overview

This guide builds on the [workload identity](egress-gateway.md#workload-identity) pattern from the egress gateway setup guide. That pattern uses `kubernetesTokenReview` to validate workload SA tokens. Credential injection extends it by adding Vault integration - using the validated identity to fetch per-workload credentials and inject them into outbound requests.

### Request Flow

1. Internal workload sends an HTTP request to the egress gateway with `Authorization: Bearer <sa-token>`
2. The gateway triggers an ext_authz check with Authorino
3. Authorino validates the workload's Kubernetes service account token via [TokenReview](https://kubernetes.io/docs/reference/kubernetes-api/authentication-resources/token-review-v1/)
4. Authorino authenticates to Vault using the workload's SA token (Vault [Kubernetes auth method](https://developer.hashicorp.com/vault/docs/auth/kubernetes))
5. Authorino reads the external credential from Vault and overwrites the `Authorization` header via `response.success.headers`
6. The egress gateway originates TLS and forwards the request with the external credential to the external service

### Topology

```mermaid
graph TB
    WL([Internal<br/>Workload])

    subgraph "gateway-system namespace"
        GW[Gateway<br/>kuadrant-egressgateway]
        HR[HTTPRoute<br/>httpbin-external]
        SE[ServiceEntry<br/>httpbin-external]
        DR[DestinationRule<br/>httpbin-external]
        AP[AuthPolicy<br/>httpbin-external-auth]
    end

    subgraph "kuadrant-system namespace"
        AUTH[Authorino]
    end

    subgraph "vault namespace"
        VAULT[Vault<br/>K8s auth]
    end

    WL -->|"1. Authorization:<br/>Bearer sa-token"| GW
    HR -.->|routes| GW
    AP -.->|attached to| HR
    GW -->|"2. ext_authz"| AUTH
    AUTH -->|"3. Vault login<br/>(SA token)"| VAULT
    AUTH -->|"4. read secret"| VAULT
    GW -->|"5. HTTPS +<br/>Bearer external-key"| EXT([External API<br/>e.g. httpbin.org])
    SE -.->|registers| EXT
    DR -.->|TLS config| EXT
```

## Prerequisites

- Egress gateway infrastructure deployed. See the [Egress Gateway Setup](egress-gateway.md) guide.
- [HashiCorp Vault](https://developer.hashicorp.com/vault) with [Kubernetes auth method](https://developer.hashicorp.com/vault/docs/auth/kubernetes) configured. The setup script deploys a dev Vault instance automatically.

```sh
export EGRESS_IP=$(kubectl get gtw kuadrant-egressgateway -n gateway-system \
    -o jsonpath='{.status.addresses[0].value}')
```

## Credential Injection

The AuthPolicy below authenticates the workload via its Kubernetes service account token, fetches a per-identity credential from Vault, and injects it into the outbound request. The Vault path is constructed dynamically from the workload's namespace and service account name, so different workloads get different credentials. The workload sends `Authorization: Bearer <sa-token>`; the gateway replaces it with `Authorization: Bearer <external-key>`.

### Step 1: Apply the AuthPolicy

```sh
kubectl apply -f - <<'EOF'
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: httpbin-external-auth
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: httpbin-external
  rules:
    authentication:
      "workload-identity":
        kubernetesTokenReview:
          audiences:
            - "https://kubernetes.default.svc.cluster.local"
    metadata:
      vault_login:
        http:
          url: "http://vault.vault.svc.cluster.local:8200/v1/auth/kubernetes/login"
          method: POST
          contentType: application/json
          body:
            expression: '"{\"jwt\": \"" + request.headers.authorization.substring(7) + "\", \"role\": \"egress-workload\"}"'
        priority: 0
      vault_secret:
        http:
          urlExpression: '"http://vault.vault.svc.cluster.local:8200/v1/secret/data/egress/" + auth.metadata.vault_login.auth.metadata.service_account_namespace + "/" + auth.metadata.vault_login.auth.metadata.service_account_name'
          method: GET
          headers:
            X-Vault-Token:
              expression: 'auth.metadata.vault_login.auth.client_token'
        priority: 1
    authorization:
      vault_credential_check:
        patternMatching:
          patterns:
          - predicate: 'has(auth.metadata.vault_secret.data)'
    response:
      success:
        headers:
          authorization:
            plain:
              expression: '"Bearer " + auth.metadata.vault_secret.data.data.api_key'
EOF
```

**How it works:**
- `authentication.workload-identity` - validates the workload's Kubernetes SA token via [TokenReview](https://kubernetes.io/docs/reference/kubernetes-api/authentication-resources/token-review-v1/). No API keys to distribute - every pod already has an SA token mounted.
- `metadata.vault_login` - POSTs the SA token to Vault's Kubernetes auth endpoint. Vault validates the token and returns a scoped client token based on the workload's namespace and service account.
- `metadata.vault_secret` - reads the external credential from Vault at a path derived from the workload's namespace and service account (e.g., `secret/egress/egress-test/default`). Each workload identity gets its own credential.
- `authorization.vault_credential_check` - verifies the Vault credential fetch succeeded. If the workload's SA isn't authorized by Vault's policy, the request is denied (403).
- `response.success.headers.authorization` - overwrites the `Authorization` header with the external credential. The workload's SA token never reaches the external service.

### Step 2: Test

The setup script stored a test credential at `secret/egress/egress-test/default` for the test client. Test the three access scenarios:

```sh
# No token - denied (401)
kubectl exec test-client -n egress-test -- curl -s -o /dev/null -w "%{http_code}" \
    -H "Host: httpbin.org" http://${EGRESS_IP}/get
# 401

# SA token from authorized namespace - credential injected
kubectl exec test-client -n egress-test -- sh -c '
curl -s -H "Host: httpbin.org" \
    -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
    http://'"${EGRESS_IP}"'/get
'
# Response shows: Authorization: Bearer sk-test-openai-key-for-egress

# SA token from unauthorized namespace - denied (403)
kubectl run bad-client --image=curlimages/curl:latest -n default --restart=Never \
    --command -- sleep infinity
kubectl wait --for=condition=Ready pod/bad-client -n default --timeout=30s
kubectl exec bad-client -n default -- sh -c '
curl -s -o /dev/null -w "%{http_code}" -H "Host: httpbin.org" \
    -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
    http://'"${EGRESS_IP}"'/get
'
# 403
kubectl delete pod bad-client -n default
```

### Per-Identity Credentials

Different workload identities automatically get different credentials from Vault. The `urlExpression` constructs the Vault path from the workload's namespace and service account name.

To verify, store a different credential for another namespace and test:

```sh
# Add workload-b namespace to the Vault role
kubectl exec vault-0 -n vault -- vault write auth/kubernetes/role/egress-workload \
    bound_service_account_names=default \
    bound_service_account_namespaces=egress-test,workload-b \
    policies=egress-read \
    ttl=1h

# Store a different credential for workload-b
kubectl exec vault-0 -n vault -- vault kv put secret/egress/workload-b/default \
    api_key=sk-different-key-for-workload-b

# Deploy a workload in workload-b namespace
kubectl create namespace workload-b
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: test-client-b
  namespace: workload-b
spec:
  containers:
    - name: curl
      image: curlimages/curl:latest
      command: ["sleep", "infinity"]
  restartPolicy: Never
EOF
kubectl wait --for=condition=Ready pod/test-client-b -n workload-b --timeout=60s

# Each workload gets its own credential
kubectl exec test-client -n egress-test -- sh -c '
curl -s -H "Host: httpbin.org" \
    -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
    http://'"${EGRESS_IP}"'/get
'
# Authorization: Bearer sk-test-openai-key-for-egress

kubectl exec test-client-b -n workload-b -- sh -c '
curl -s -H "Host: httpbin.org" \
    -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
    http://'"${EGRESS_IP}"'/get
'
# Authorization: Bearer sk-different-key-for-workload-b
```

## Security

Access control is enforced at two layers:

1. **Kubernetes TokenReview** - Authorino validates the workload's SA token. Requests without a valid token are rejected (401).
2. **Vault Kubernetes auth** - Vault validates the SA token independently and checks it against the role's `bound_service_account_names` and `bound_service_account_namespaces`. Only workloads in authorized namespaces get credentials. Unauthorized workloads are rejected (403).

No shared tokens or API keys are involved. Each workload authenticates with its own Kubernetes identity and receives its own credential from a per-identity Vault path.

**Credential management**: External API keys must be stored in Vault at the correct per-identity path (`secret/egress/<namespace>/<sa-name>`) before workloads can use them. This is typically a platform team responsibility. To allow workload teams to manage their own credentials, grant scoped Vault write access per namespace (e.g., `secret/data/egress/<namespace>/*`).

## Cleanup

```sh
kubectl delete authpolicy httpbin-external-auth -n gateway-system --ignore-not-found
kubectl delete pod bad-client -n default --ignore-not-found
kubectl delete pod test-client-b -n workload-b --ignore-not-found
kubectl delete namespace workload-b --ignore-not-found
```

To tear down the egress gateway infrastructure and Vault:

```sh
curl -sL https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/hack/setup-egress.sh | bash -s cleanup
```

## References

- [Egress Gateway Setup](egress-gateway.md)
- [DNS Routing Guide](dns-routing.md)
- [RFC 0016: Egress Gateway](https://github.com/Kuadrant/architecture/blob/main/rfcs/0016-egress-gateway.md)
- [AuthPolicy Overview](../../overviews/auth.md)
- [Vault Kubernetes Auth Method](https://developer.hashicorp.com/vault/docs/auth/kubernetes)
