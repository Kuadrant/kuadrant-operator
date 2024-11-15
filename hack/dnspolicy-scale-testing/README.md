## DNSPolicy Scale Testing

Create two kind clusters with Kuadrant installed:
```bash
CLUSTER_COUNT=2 ./hack/dnspolicy-scale-testing/test.sh kind-create
```

Installed namespaced dns operators (optional):
```bash
CLUSTER_COUNT=2 ./hack/dnspolicy-scale-testing/test.sh kind-install-namespaced-dns-operator
```

Run a test:
```bash
CLUSTER_COUNT=2 TEST_NS_COUNT=5 ./hack/dnspolicy-scale-testing/test.sh test_dnspolicy_loadbalanced aws
```

Cleanup after test:

Delete all dnspolices created by tests first and ensure all dnsrecords are removed
```shell
kubectl get dnspolicy -l kuadrant.io/test-suite=manual -A --context kind-kuadrant-local-1
kubectl delete dnspolicy -l kuadrant.io/test-suite=manual -A --context kind-kuadrant-local-1
kubectl get dnsrecord -A --context kind-kuadrant-local-1
```

Delete all other test suite resources
```shell
kubectl get all,httproutes,gateway,tlspolicy,secrets,issuers -l kuadrant.io/test-suite=manual -A --context kind-kuadrant-local-1
kubectl delete all,httproutes,gateway,tlspolicy,secrets,issuers -l kuadrant.io/test-suite=manual -A --context kind-kuadrant-local-1
```
