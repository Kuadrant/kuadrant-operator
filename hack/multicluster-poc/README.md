
## Multi cluster POC Test

### Cluster Setup

Create two clusters with Kuadrant deployed
```shell
CLUSTER_COUNT=2 ./hack/multicluster-poc/create-clusters.sh
kubectl config get-contexts
```

Scale down the dns-operator deployment on cluster 2 (Fairly sure this will cause issues currently)
```shell
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

Tail the dns operator logs on Cluster 1 (Optional)
```shell
kubectl stern deployment/dns-operator-controller-manager -n kuadrant-system --context kind-kuadrant-local-1
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

Verify CoreDNS is accessible on Cluster 1, and the example domain exists:
```shell
CORE_NS=`kubectl get service -A -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics --context kind-kuadrant-local-1 -o json | jq -r '[.items[] | (.status.loadBalancer.ingress[].ip)][0]'`
dig @$CORE_NS -t AXFR k.example.com +noall +answer
```
Expected output:
```
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
k.example.com.          60      IN      NS      ns1.k.example.com.
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
```

>Note: Ignore the extra SOA record :-)

Add Cluster 2 kubeconfig secret 
```shell
docker run --rm -u $UID -v /home/mnairn/go/src/github.com/kuadrant/kuadrant-operator/hack/multicluster-poc:/tmp/multicluster-poc:z --network kind -e KUBECONFIG=/tmp/multicluster-poc/kubeconfigs/kuadrant-local-all.internal.kubeconfig alpine/k8s:1.30.13 /tmp/multicluster-poc/create-kubeconfig-secret.sh
```

>Note: This is running in a container in order for the script to properly communicate with the remote and primary servers and generate a config that will work form inside the kind cluster

Verify cluster secret exists on the cluster 1 only
```shell
kubectl get secrets -A -l sigs.k8s.io/multicluster-runtime-kubeconfig=true --context kind-kuadrant-local-1
kubectl get secrets -A -l sigs.k8s.io/multicluster-runtime-kubeconfig=true --context kind-kuadrant-local-2
```

#### Run a test

Deploy gateways, routes and services
```shell
CLUSTER_COUNT=2 ../../hack/multicluster-poc/test.sh
```

Verify deployed resources
```shell
kubectl get all,gateway,httproute,secret,dnspolicy -A -l kuadrant.io/test=multicluster-poc --context kind-kuadrant-local-1
kubectl get all,gateway,httproute,secret,dnspolicy -A -l kuadrant.io/test=multicluster-poc --context kind-kuadrant-local-2
```

##### Simple Record (A Records only)

Create a simple delegated record with a primary role on cluster 1
```shell
kubectl apply -f ../../hack/multicluster-poc/primary/dnspolicy/dnspolicy_prod-web-istio-simple.yaml -n dnstest-1 --context kind-kuadrant-local-1
````


Create a simple delegated record with a remote role on cluster 2
```shell
kubectl apply -f ../../hack/multicluster-poc/remote/dnspolicy/dnspolicy_prod-web-istio-simple.yaml -n dnstest-1 --context kind-kuadrant-local-2
````

Verify records on cluster 1 (Primary)
```shell
kubectl get dnsrecords --context kind-kuadrant-local-1 -n dnstest-1 --show-labels
```
Expected output:
```
NAME                                       READY   LABELS
prod-web-istio-coredns-api                 True    app.kubernetes.io/component=kuadrant,app.kubernetes.io/instance=kuadrant,app.kubernetes.io/managed-by=kuadrant-operator,app.kubernetes.io/name=kuadrant,app.kubernetes.io/part-of=kuadrant,app=kuadrant,kuadrant.io/listener-name=api
prod-web-istio-coredns-api-authoritative   True    kuadrant.io/authoritative-record=myapp.k.example.com,kuadrant.io/coredns-zone-name=k.example.com
```

Verify records on cluster 2 (Remote)
```shell
kubectl get dnsrecords --context kind-kuadrant-local-2 -n dnstest-1 --show-labels
```
Expected output:
```
NAME                         READY   LABELS
prod-web-istio-coredns-api   True    app.kubernetes.io/component=kuadrant,app.kubernetes.io/instance=kuadrant,app.kubernetes.io/managed-by=kuadrant-operator,app.kubernetes.io/name=kuadrant,app.kubernetes.io/part-of=kuadrant,app=kuadrant,kuadrant.io/listener-name=api
```


Verify endpoints of authoritative record on cluster 1 (Primary)
```shell
kubectl get dnsrecord/prod-web-istio-coredns-api-authoritative --context kind-kuadrant-local-1 -n dnstest-1 -o json | jq '.spec.endpoints[] | "dnsName: \(.dnsName), recordType: \(.recordType), targets: \(.targets), labels: \(.labels)"'
```
Expected output:
```
"dnsName: myapp.k.example.com, recordType: SOA, targets: null, labels: null"
"dnsName: myapp.k.example.com, recordType: A, targets: [\"172.18.0.18\",\"172.18.0.33\"], labels: {\"owner\":\"15p262su&&tl6a98es\"}"
"dnsName: kuadrant-a-myapp.k.example.com, recordType: TXT, targets: [\"\\\"heritage=external-dns,external-dns/owner=15p262su&&tl6a98es\\\"\"], labels: {\"ownedRecord\":\"myapp.k.example.com\"}"
```

Verify CoreDNS records on cluster 1 (Primary)
```shell
CORE_NS=`kubectl get service -A -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics --context kind-kuadrant-local-1 -o json | jq -r '[.items[] | (.status.loadBalancer.ingress[].ip)][0]'`
dig @$CORE_NS -t AXFR k.example.com +noall +answer
```
Expected output:
```
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
k.example.com.          60      IN      NS      ns1.k.example.com.
kuadrant-a-myapp.k.example.com. 0 IN    TXT     "\"heritage=external-dns,external-dns/owner=15p262su&&tl6a98es\""
myapp.k.example.com.    60      IN      A       172.18.0.18
myapp.k.example.com.    60      IN      A       172.18.0.33
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
```

Cleanup:
```shell
kubectl delete -f hack/multicluster-poc/remote/dnspolicy/dnspolicy_prod-web-istio-simple.yaml -n dnstest-1 --context kind-kuadrant-local-2
kubectl delete -f hack/multicluster-poc/primary/dnspolicy/dnspolicy_prod-web-istio-simple.yaml -n dnstest-1 --context kind-kuadrant-local-1
kubectl delete dnsrecord/prod-web-istio-coredns-api-authoritative -n dnstest-1 --context kind-kuadrant-local-1
```


##### LoadBalanced Record (A Records only)

Create a delegated record with a primary role on cluster 1
```shell
kubectl apply -f hack/multicluster-poc/primary/dnspolicy/dnspolicy_prod-web-istio-loadbalanced.yaml -n dnstest-1 --context kind-kuadrant-local-1
````

Create a simple delegated record with a remote role on cluster 2
```shell
kubectl apply -f hack/multicluster-poc/remote/dnspolicy/dnspolicy_prod-web-istio-loadbalanced.yaml -n dnstest-1 --context kind-kuadrant-local-2
````

Verify records on cluster 1 (Primary)
```shell
kubectl get dnsrecords --context kind-kuadrant-local-1 -n dnstest-1 --show-labels
```
Expected output:
```
NAME                                       READY   LABELS
prod-web-istio-coredns-api                 True    app.kubernetes.io/component=kuadrant,app.kubernetes.io/instance=kuadrant,app.kubernetes.io/managed-by=kuadrant-operator,app.kubernetes.io/name=kuadrant,app.kubernetes.io/part-of=kuadrant,app=kuadrant,kuadrant.io/listener-name=api
prod-web-istio-coredns-api-authoritative   True    kuadrant.io/authoritative-record=myapp.k.example.com,kuadrant.io/coredns-zone-name=k.example.com
```

Verify records on cluster 2 (Remote)
```shell
kubectl get dnsrecords --context kind-kuadrant-local-2 -n dnstest-1 --show-labels
```
Expected output:
```shell
NAME                         READY   LABELS
prod-web-istio-coredns-api   True    app.kubernetes.io/component=kuadrant,app.kubernetes.io/instance=kuadrant,app.kubernetes.io/managed-by=kuadrant-operator,app.kubernetes.io/name=kuadrant,app.kubernetes.io/part-of=kuadrant,app=kuadrant,kuadrant.io/listener-name=api
```

Verify endpoints of authoritative record on cluster 1 (Primary)
```shell
kubectl get dnsrecord/prod-web-istio-coredns-api-authoritative --context kind-kuadrant-local-1 -n dnstest-1 -o json | jq '.spec.endpoints[] | "dnsName: \(.dnsName), recordType: \(.recordType), targets: \(.targets), labels: \(.labels)"'
```
Expected output:
```
"dnsName: myapp.k.example.com, recordType: SOA, targets: null, labels: null"
"dnsName: klb.myapp.k.example.com, recordType: CNAME, targets: [\"us.klb.myapp.k.example.com\"], labels: {\"owner\":\"34xubq3u\"}"
"dnsName: myapp.k.example.com, recordType: CNAME, targets: [\"klb.myapp.k.example.com\"], labels: {\"owner\":\"34xubq3u&&eypir7wu\"}"
"dnsName: us.klb.myapp.k.example.com, recordType: CNAME, targets: [\"21xkck-2frmk1.klb.myapp.k.example.com\"], labels: {\"owner\":\"34xubq3u\"}"
"dnsName: 21xkck-2frmk1.klb.myapp.k.example.com, recordType: A, targets: [\"172.18.0.18\"], labels: {\"owner\":\"34xubq3u\"}"
"dnsName: klb.myapp.k.example.com, recordType: CNAME, targets: [\"us.klb.myapp.k.example.com\"], labels: {\"owner\":\"34xubq3u\"}"
"dnsName: kuadrant-cname-klb.myapp.k.example.com, recordType: TXT, targets: [\"\\\"heritage=external-dns,external-dns/owner=34xubq3u\\\"\"], labels: {\"ownedRecord\":\"klb.myapp.k.example.com\"}"
"dnsName: kuadrant-cname-myapp.k.example.com, recordType: TXT, targets: [\"\\\"heritage=external-dns,external-dns/owner=34xubq3u&&eypir7wu\\\"\"], labels: {\"ownedRecord\":\"myapp.k.example.com\"}"
"dnsName: kuadrant-cname-us.klb.myapp.k.example.com, recordType: TXT, targets: [\"\\\"heritage=external-dns,external-dns/owner=34xubq3u\\\"\"], labels: {\"ownedRecord\":\"us.klb.myapp.k.example.com\"}"
"dnsName: kuadrant-a-21xkck-2frmk1.klb.myapp.k.example.com, recordType: TXT, targets: [\"\\\"heritage=external-dns,external-dns/owner=34xubq3u\\\"\"], labels: {\"ownedRecord\":\"21xkck-2frmk1.klb.myapp.k.example.com\"}"
"dnsName: kuadrant-cname-klb.myapp.k.example.com, recordType: TXT, targets: [\"\\\"heritage=external-dns,external-dns/owner=34xubq3u\\\"\"], labels: {\"ownedRecord\":\"klb.myapp.k.example.com\"}"
"dnsName: 1bfk41-2frmk1.klb.myapp.k.example.com, recordType: A, targets: [\"172.18.0.33\"], labels: {\"owner\":\"eypir7wu\"}"
"dnsName: ie.klb.myapp.k.example.com, recordType: CNAME, targets: [\"1bfk41-2frmk1.klb.myapp.k.example.com\"], labels: {\"owner\":\"eypir7wu\"}"
"dnsName: klb.myapp.k.example.com, recordType: CNAME, targets: [\"ie.klb.myapp.k.example.com\"], labels: {\"owner\":\"eypir7wu\"}"
"dnsName: kuadrant-a-1bfk41-2frmk1.klb.myapp.k.example.com, recordType: TXT, targets: [\"\\\"heritage=external-dns,external-dns/owner=eypir7wu\\\"\"], labels: {\"ownedRecord\":\"1bfk41-2frmk1.klb.myapp.k.example.com\"}"
"dnsName: kuadrant-cname-ie.klb.myapp.k.example.com, recordType: TXT, targets: [\"\\\"heritage=external-dns,external-dns/owner=eypir7wu\\\"\"], labels: {\"ownedRecord\":\"ie.klb.myapp.k.example.com\"}"
"dnsName: kuadrant-cname-klb.myapp.k.example.com, recordType: TXT, targets: [\"\\\"heritage=external-dns,external-dns/owner=eypir7wu\\\"\"], labels: {\"ownedRecord\":\"klb.myapp.k.example.com\"}"
```

Verify CoreDNS records on cluster 1 (Primary)
```shell
CORE_NS=`kubectl get service -A -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics --context kind-kuadrant-local-1 -o json | jq -r '[.items[] | (.status.loadBalancer.ingress[].ip)][0]'`
dig @$CORE_NS -t AXFR k.example.com +noall +answer
```
Expected output:
```
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
k.example.com.          60      IN      NS      ns1.k.example.com.
kuadrant-cname-myapp.k.example.com. 0 IN TXT    "\"heritage=external-dns,external-dns/owner=34xubq3u&&eypir7wu\""
myapp.k.example.com.    300     IN      CNAME   klb.myapp.k.example.com.
klb.myapp.k.example.com. 300    IN      CNAME   us.klb.myapp.k.example.com.
klb.myapp.k.example.com. 300    IN      CNAME   us.klb.myapp.k.example.com.
klb.myapp.k.example.com. 300    IN      CNAME   ie.klb.myapp.k.example.com.
1bfk41-2frmk1.klb.myapp.k.example.com. 60 IN A  172.18.0.33
21xkck-2frmk1.klb.myapp.k.example.com. 60 IN A  172.18.0.18
ie.klb.myapp.k.example.com. 60  IN      CNAME   1bfk41-2frmk1.klb.myapp.k.example.com.
kuadrant-a-1bfk41-2frmk1.klb.myapp.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=eypir7wu\""
kuadrant-a-21xkck-2frmk1.klb.myapp.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=34xubq3u\""
kuadrant-cname-ie.klb.myapp.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=eypir7wu\""
kuadrant-cname-us.klb.myapp.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=34xubq3u\""
us.klb.myapp.k.example.com. 60  IN      CNAME   21xkck-2frmk1.klb.myapp.k.example.com.
kuadrant-cname-klb.myapp.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=34xubq3u\""
kuadrant-cname-klb.myapp.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=34xubq3u\""
kuadrant-cname-klb.myapp.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=eypir7wu\""
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
```

Cleanup:
```shell
kubectl delete -f hack/multicluster-poc/remote/dnspolicy/dnspolicy_prod-web-istio-loadbalanced.yaml -n dnstest-1 --context kind-kuadrant-local-2
kubectl delete -f hack/multicluster-poc/primary/dnspolicy/dnspolicy_prod-web-istio-loadbalanced.yaml -n dnstest-1 --context kind-kuadrant-local-1
kubectl delete dnsrecord/prod-web-istio-coredns-api-authoritative -n dnstest-1 --context kind-kuadrant-local-1
```