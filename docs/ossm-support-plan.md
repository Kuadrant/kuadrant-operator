# Plan: Add OSSM 3.x support to CRC Istio setup

## Context

The CRC setup currently installs Istio via the upstream Sail Helm chart. This plan adds optional support for OpenShift Service Mesh (OSSM) 3.x instead — Red Hat's supported Istio distribution, installed via OLM. OSSM 3.x uses the same Sail operator API (`sailoperator.io/v1`) under the hood, so the Istio CR and Gateway resources are compatible.

The existing Sail install uses `helm install sail-operator` + an `Istio` CR. OSSM instead uses an OLM `Subscription` to `servicemeshoperator3` from `redhat-operators`, plus an `IstioCNI` resource (required on OpenShift for non-privileged network interception).

## Approach

Add a `CRC_ISTIO_METHOD` variable (`sail` or `ossm`, default `sail`) that controls which install path `crc-istio-env-setup` uses.

### New manifests: `config/dependencies/istio/ossm/`

**`subscription.yaml`** — OLM Subscription for OSSM 3.x:
```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: servicemeshoperator3
  namespace: openshift-operators
spec:
  channel: stable
  installPlanApproval: Automatic
  name: servicemeshoperator3
  source: redhat-operators
  sourceNamespace: openshift-marketplace
```

**`istiocni.yaml`** — IstioCNI (required, must be applied before the Istio CR):
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: istio-cni
---
apiVersion: sailoperator.io/v1
kind: IstioCNI
metadata:
  name: default
spec:
  namespace: istio-cni
  profile: openshift
```

**`istio.yaml`** — Istio CR (mirrors the existing Sail one but without pinned version, since OSSM manages versions through operator updates):
```yaml
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: default
spec:
  namespace: istio-system
  updateStrategy:
    type: InPlace
  values:
    pilot:
      autoscaleEnabled: false
```

### New make target: `ossm-install` in `make/istio.mk`

```makefile
.PHONY: ossm-install
ossm-install:
	kubectl apply -f $(ISTIO_INSTALL_DIR)/ossm/subscription.yaml
	@echo "[INFO] Waiting for Sail operator CRDs from OSSM..."
	kubectl wait --for=condition=Established crd/istios.sailoperator.io --timeout=300s
	kubectl apply -f $(ISTIO_INSTALL_DIR)/ossm/istiocni.yaml
	kubectl apply -f $(ISTIO_INSTALL_DIR)/ossm/istio.yaml

.PHONY: ossm-uninstall
ossm-uninstall:
	-kubectl delete -f $(ISTIO_INSTALL_DIR)/ossm/istio.yaml
	-kubectl delete -f $(ISTIO_INSTALL_DIR)/ossm/istiocni.yaml
	-kubectl delete -f $(ISTIO_INSTALL_DIR)/ossm/subscription.yaml
```

### Update `crc-istio-env-setup` in `make/crc.mk`

Add variable:
```makefile
CRC_ISTIO_METHOD ?= sail
```

Update `crc-istio-env-setup` to dispatch based on `CRC_ISTIO_METHOD`:
- `sail` — existing behavior (check `helm status`, call `istio-install`)
- `ossm` — check if OSSM subscription exists, call `ossm-install` if not

The gateway deployment (`deploy-istio-gateway`) is unchanged — OSSM 3.x registers the same `istio` GatewayClass.

## Files to modify

- **`make/istio.mk`** — add `ossm-install` and `ossm-uninstall` targets
- **`make/crc.mk`** — add `CRC_ISTIO_METHOD` variable, update `crc-istio-env-setup` dispatch logic
- **Create `config/dependencies/istio/ossm/subscription.yaml`**
- **Create `config/dependencies/istio/ossm/istiocni.yaml`**
- **Create `config/dependencies/istio/ossm/istio.yaml`**

## Usage

```bash
# Default — upstream Sail via Helm (existing behavior)
make crc-setup GATEWAYAPI_PROVIDER=istio

# OSSM 3.x via OLM
make crc-setup GATEWAYAPI_PROVIDER=istio CRC_ISTIO_METHOD=ossm
```

## Verification

1. `make crc-setup GATEWAYAPI_PROVIDER=istio CRC_ISTIO_METHOD=ossm`
2. `oc get csv -n openshift-operators` — OSSM operator CSV should be `Succeeded`
3. `oc get istiocni -A` — IstioCNI `default` should be ready
4. `oc get istio -A` — Istio `default` should be ready
5. `kubectl get pods -n istio-system` — istiod pod running
6. `kubectl get gatewayclass istio` — GatewayClass registered
7. `kubectl get gateway -n gateway-system` — ingressgateway programmed

## References

- [OSSM 3.0 Install Docs](https://docs.redhat.com/en/documentation/red_hat_openshift_service_mesh/3.0/html/installing/ossm-installing-service-mesh)
- [Integrate OpenShift Gateway API with OSSM](https://developers.redhat.com/articles/2025/12/09/integrate-openshift-gateway-api-openshift-service-mesh)
- [Introducing OSSM 3.0 (Red Hat Blog)](https://www.redhat.com/en/blog/introducing-red-hat-openshift-service-mesh-3)
