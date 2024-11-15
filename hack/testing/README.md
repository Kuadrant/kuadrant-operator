

Create two kind clusters (Minimal setup, no kuadrant):
```bash
CLUSTER_COUNT=2 ./hack/testing/test.sh kind-create
```

Generate kuadrant deployment overlays for each cluster:
```bash
CLUSTER_COUNT=2 ./hack/testing/test.sh generate-cluster-overlay
```

Apply kuadrant deployment overlays for each cluster:
```bash
CLUSTER_COUNT=2 ./hack/testing/test.sh apply-cluster-overlay
```

Run a test:
```bash
CLUSTER_COUNT=2 TEST_NS_COUNT=1 ./hack/testing/test.sh test_dnspolicy_simple
```
