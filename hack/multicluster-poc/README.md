
## Multi cluster POC Test

```shell
cd /home/mnairn/go/src/github.com/kuadrant/kuadrant-operator
git checkout multicluster-poc
```

### Cluster Setup

Create two clusters with Kuadrant deployed
```shell
CLUSTER_COUNT=2 ./hack/multicluster-poc/create-clusters.sh
kubectl config get-contexts
```

Scale down all dns-operator deployments so we can just use `make run` locally against the primary
```shell
kubectl scale deployment/dns-operator-controller-manager -n kuadrant-system --replicas=0 --context kind-kuadrant-local-1
kubectl scale deployment/dns-operator-controller-manager -n kuadrant-system --replicas=0 --context kind-kuadrant-local-2
```

Tail the kuadrant operator logs on Cluster 1 (Optional)
```shell
kubectl stern deployment/kuadrant-operator-controller-manager -n kuadrant-system --context kind-kuadrant-local-1
```

Tail the kuadrant operator logs on Cluster 2 (Optional)
```shell
kubectl stern deployment/kuadrant-operator-controller-manager -n kuadrant-system --context kind-kuadrant-local-2
```

Watch the topology on Cluster 1 (Optional)
```shell
watch "kubectl get configmap/topology -n kuadrant-system -o jsonpath=\"{.data['topology']}\" --context kind-kuadrant-local-1 > topology-cluster-1.dot"
```

Watch the topology on Cluster 2 (Optional)
```shell
watch "kubectl get configmap/topology -n kuadrant-system -o jsonpath=\"{.data['topology']}\" --context kind-kuadrant-local-2 > topology-cluster-2.dot"
```

#### Setup Primary Cluster

The first cluster will be assigned the "primary" role which means:
* CoreDNS deployed
* Cluster 2 kubeconfig added
* Provider secrets added in test namespace

Deploy CoreDNS
```shell
../../bin/kustomize build --enable-helm https://github.com/Kuadrant/dns-operator/config/coredns | kubectl apply --context kind-kuadrant-local-1 -f -
```

Verify CoreDNS running on Cluster 1 only
```shell
kubectl get deployments -l app.kubernetes.io/name=coredns -A --context kind-kuadrant-local-1
kubectl get deployments -l app.kubernetes.io/name=coredns -A --context kind-kuadrant-local-2
```

Add Cluster 2 kubeconfig secret 
```shell
kubectl config use-context kind-kuadrant-local-1
curl -s https://raw.githubusercontent.com/Kuadrant/dns-operator/refs/heads/multicluster-poc/hack/create-kubeconfig-secret.sh | bash -s - -c kind-kuadrant-local-2 -a dns-operator-controller-manager -n kuadrant-system
```

Verify the secret exists on the cluster with the correct label
```shell
kubectl get secrets -A -l sigs.k8s.io/multicluster-runtime-kubeconfig=true
```
Expected output:
```shell
NAMESPACE         NAME                    TYPE     DATA   AGE
kuadrant-system   kind-kuadrant-local-2   Opaque   1      53s
```

Run the dns operator on the primary
```shell
kubectl config use-context kind-kuadrant-local-1
make run
```


#### Run a test


Deploy gateways, routes and services
```shell
CLUSTER_COUNT=2 ./hack/multicluster-poc/test.sh
```

```shell
kubectl get all,gateway,httproute,secret,dnspolicy -A -l kuadrant.io/test=multicluster-poc --context kind-kuadrant-local-1
kubectl get all,gateway,httproute,secret,dnspolicy -A -l kuadrant.io/test=multicluster-poc --context kind-kuadrant-local-2
```











```shell
# Apply delegated record with role primary on the first cluster
kubectl apply -f scratch/dnsrecords/multicluster/coredns/dnsrecord-test-coredns-primary.yaml -n dnstest --context kind-kuadrant-dns-local-1

# Verify records as expected
kubectl get dnsrecord -A --show-labels -A --context kind-kuadrant-dns-local-1
NAMESPACE   NAME                                           READY   LABELS
dnstest     dnsrecord-test-coredns-primary                 True    <none>
dnstest     dnsrecord-test-coredns-primary-authoritative   True    kuadrant.io/authoritative-record=k.example.com,kuadrant.io/coredns-zone-name=k.example.com

# Verify CoreDNS records on first(primary) cluster
dig @172.18.0.17 -t AXFR k.example.com

; <<>> DiG 9.18.28 <<>> @172.18.0.17 -t AXFR k.example.com
; (1 server found)
;; global options: +cmd
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
k.example.com.          60      IN      NS      ns1.k.example.com.
k.example.com.          60      IN      CNAME   lb.dnsrecord-test.k.example.com.
cluster1.dnsrecord-test.k.example.com. 60 IN A  127.0.0.2
cluster2.dnsrecord-test.k.example.com. 60 IN A  127.0.0.3
kuadrant-a-cluster1.dnsrecord-test.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=rmwie2qo\""
kuadrant-a-cluster2.dnsrecord-test.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=rmwie2qo\""
kuadrant-cname-lb.dnsrecord-test.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=rmwie2qo\""
kuadrant-cname-lb.dnsrecord-test.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=rmwie2qo\""
lb.dnsrecord-test.k.example.com. 60 IN  CNAME   cluster1.dnsrecord-test.k.example.com.
lb.dnsrecord-test.k.example.com. 60 IN  CNAME   cluster2.dnsrecord-test.k.example.com.
kuadrant-cname-k.example.com. 0 IN      TXT     "\"heritage=external-dns,external-dns/owner=rmwie2qo\""
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
;; Query time: 0 msec
;; SERVER: 172.18.0.17#53(172.18.0.17) (TCP)
;; WHEN: Fri Jun 13 16:16:43 IST 2025
;; XFR size: 13 records (messages 1, bytes 1142)
```

```shell
# Apply delegated record with role remote on the second cluster
kubectl apply -f scratch/dnsrecords/multicluster/coredns/dnsrecord-test-coredns-remote.yaml -n dnstest --context kind-kuadrant-dns-local-2

# Verify records as expected
kubectl get dnsrecord -A --show-labels -A --context kind-kuadrant-dns-local-1
NAMESPACE   NAME                                           READY   LABELS
dnstest     dnsrecord-test-coredns-primary                 True    <none>
dnstest     dnsrecord-test-coredns-primary-authoritative   True    kuadrant.io/authoritative-record=k.example.com,kuadrant.io/coredns-zone-name=k.example.com
mnairn@deacon dns-operator (multicluster-poc) $ kubectl get dnsrecord -A --show-labels -A --context kind-kuadrant-dns-local-2
NAMESPACE   NAME                       READY   LABELS
dnstest     dnsrecord-coredns-remote   True    <none>

# Verify CoreDNS records on first(primary) cluster
dig @172.18.0.17 -t AXFR k.example.com

; <<>> DiG 9.18.28 <<>> @172.18.0.17 -t AXFR k.example.com
; (1 server found)
;; global options: +cmd
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
k.example.com.          60      IN      NS      ns1.k.example.com.
k.example.com.          60      IN      CNAME   lb.dnsrecord-test.k.example.com.
cluster1.dnsrecord-test.k.example.com. 60 IN A  127.0.0.2
cluster2.dnsrecord-test.k.example.com. 60 IN A  127.0.0.3
cluster3.dnsrecord-test.k.example.com. 60 IN A  127.0.0.4
kuadrant-a-cluster1.dnsrecord-test.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=rmwie2qo\""
kuadrant-a-cluster2.dnsrecord-test.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=rmwie2qo\""
kuadrant-a-cluster3.dnsrecord-test.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=mmfthslu\""
kuadrant-cname-lb.dnsrecord-test.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=rmwie2qo\""
kuadrant-cname-lb.dnsrecord-test.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=rmwie2qo\""
kuadrant-cname-lb.dnsrecord-test.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=mmfthslu\""
lb.dnsrecord-test.k.example.com. 60 IN  CNAME   cluster1.dnsrecord-test.k.example.com.
lb.dnsrecord-test.k.example.com. 60 IN  CNAME   cluster2.dnsrecord-test.k.example.com.
lb.dnsrecord-test.k.example.com. 60 IN  CNAME   cluster3.dnsrecord-test.k.example.com.
kuadrant-cname-k.example.com. 0 IN      TXT     "\"heritage=external-dns,external-dns/owner=mmfthslu&&rmwie2qo\""
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
;; Query time: 1 msec
;; SERVER: 172.18.0.17#53(172.18.0.17) (TCP)
;; WHEN: Fri Jun 13 16:20:11 IST 2025
;; XFR size: 17 records (messages 1, bytes 1509)


```