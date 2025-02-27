## Try out coredns with Kuadrant

Kuadrant's DNS capabilities can be used with [CoreDNS](https://coredns.io/) at a proof of concept level. With this guide, you will be able to use the Kuadrant `DNSPolicy` and leverage CoreDns as the authoritative nameserver(s) for a given domain. We will show how it can be used with multiple gateways and provide you with both a weighted and GEO response similar to that offered by the cloud providers.


>Note: This is uses a proof of concept and is intended only for experimenting. It is not currently a core part of the kuadrant DNS offering.


The basic architecture is shown below:

[!image](./core-dns.png)


For this local setup however we are going to use a single cluster rather than multiple to reduce the setup overhead. The same architecture is true, but rather than a core dns per cluster we have one per namespace. For this guide, we will have two instances of core dns.


### Setup a local Kuadrant development environment with CoreDNS enabled

We will need to setup several pieces including metallb, so to try this out, clone the kuadrant-operator repo first.

```
git clone https://github.com/Kuadrant/kuadrant-operator.git
```

From the root of the kuadrant-operator repo execute:

```
make local-setup DNS_OPERATOR_GITREF=coredns && ./bin/kustomize build --enable-helm https://github.com/Kuadrant/dns-operator/config/coredns-multi?ref=coredns | kubectl apply -f -
```


Once you have Kuadrant installed, we will need to enable the CoreDNS provider integration and disable the health checks probes (not currently a part of the core dns integration):


Update the flags to the container:

```
kubectl patch deployment dns-operator-controller-manager  --type='json' -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/args", "value": ["--metrics-bind-address=:8080", "--leader-elect","--provider=coredns","--enable-probes=false"]}]'  -n kuadrant-system
```

To ensure stability, set the image of kuadrant-operator to the v1.0.2 rather than using main. If you want to use latest just set the tag to `latest` instead. 

```
kubectl set image deployment/kuadrant-operator-controller-manager *=quay.io/kuadrant/kuadrant-operator:v1.0.2 -n kuadrant-system
```


The environment is now setup. Next we will setup some gateways and DNS Policies that will trigger the creation of core dns handled DNSRecords:

### Setup the Gateways

To show this working but keep the setup simple and in a single cluster, we will now setup 2 gateways on the same cluster. Two of these gateways we will use DNSPolicy to define their location as in the EU continent and the other of these gateways we will use DNSPolicy to define as in the NA continent. Later on we will update these to show weighted DNSResponse working.

As we have multiple instances of CoreDNS running and they are namespace scoped, we need to use the same namespaces as as the dns servers for our gateways and policies as each dns server is configured to look only at the same namespace for DNSRecords to handle. For convenience we have some gateways already setup.

> Note: these use the k.example.com domain. This is also pre-configured in the core dns corefile as a zone that uses the kuadrant CoreDns plugin. The Kuadran CoreDNS Plugin is responsible for reading the DNSRecord resources and applying GEO location , Weighting or both Geo location and weighting when there is more than one gateway within a single geo.


```

kubectl apply -f examples/coredns/gateways.yaml

```

### Setup the DNSProvider secrets

To setup the DNSProvider secrets, we need to know the external IPAddress of each CoreDNS instance. This is to allow the DNS Operator to query each nameserver for its records to form a full record set for a given dns name.

```
export coredns1IP=$(kubectl get service kuadrant-coredns-1 -n kuadrant-coredns-1 -o=jsonpath='{.status.loadBalancer.ingress[0].ip}')
export coredns2IP=$(kubectl get service kuadrant-coredns-2 -n kuadrant-coredns-2 -o=jsonpath='{.status.loadBalancer.ingress[0].ip}')
```

we want to set the nameservers in the provider secret:

```
# we are setting the nameserver of coredns-2 in the provider of secret for coredns-1. You may need to adjust the IPAddress. The port being present is important 

kubectl create secret generic core-dns --namespace=kuadrant-coredns-1 --type=kuadrant.io/coredns --from-literal=NAMESERVERS="${coredns1IP}:53,${coredns2IP}:53" --from-literal=ZONES="k.example.com"

# we are setting the nameserver of coredns-1 in the provider of secret for coredns-2. You may need to adjust the IPAddress. The port being present is important 

kubectl create secret generic core-dns --namespace=kuadrant-coredns-2 --type=kuadrant.io/coredns --from-literal=NAMESERVERS="${coredns2IP}:53,${coredns1IP}:53" --from-literal=ZONES="k.example.com"
```

### Setup the DNSPolicies

Now that we have our gateways and providers in place. Next we can setup the DNSPolicies. For convenience we have created an example for this guide.

>Note: We create 2 DNSPolicies. In a real world scenario each of these gateways and policies would likely be on a different cluster. In the 2 DNSPolicies the main difference is the one targeting the NA gateway has a GEO set of NA while the one targeting the EU gateway has a GEO set of EU.

```
kubectl apply -f examples/coredns/dnspolicies.yaml
```

### Validate and test the setup

You can verify that everything is as expected by checking the DNSPolicy status. It is likely to take a minute or so before the status is marked as enforced but you should see enforced true for each policy.

```
kubectl get dnspolicy -A -o=wide
```

Once they are all enforced. We can issue some queries against our local authoritative nameservers. 

>Note: as this is a local setup we have not configured an edge recursive DNS server. In a real world scenario, a zone would be delegated to these CoreDNS instances from a dns provider such as AWS route53

### Test GEO based routing

For testing purposes we have populated a fake geo database with a specific CIDR for each geo. It works the same as a normal GEO based database just is much smaller.

>Note you can use either instance of the CoreDNS servers they will both return the same result

#### EU based geo.

We specify the subnet here to ensure that core dns sees this as the originating IP which we have mapped to the EU region in our database.

```
dig @${coredns1IP} k.example.com +subnet=127.0.100.100 +short
dig @${coredns2IP} k.example.com +subnet=127.0.100.100 +short
```

Below is a sample output

```
dig @${coredns2IP} k.example.com +subnet=127.0.100.100 +short
klb.k.example.com.
geo-eu.klb.k.example.com.
1fopkr-332u3t.klb.k.example.com.
10.89.0.20

dig @${coredns1IP} k.example.com +subnet=127.0.100.100 +short
klb.k.example.com.
geo-eu.klb.k.example.com.
1fopkr-332u3t.klb.k.example.com.
10.89.0.20
```

#### US based geo.

To test US based geo we just need to update the subnet and hit the namesevers again:

```
dig @${coredns1IP} k.example.com +subnet=127.0.200.200 +short
dig @${coredns2IP} k.example.com +subnet=127.0.200.200 +short
```

Below is a sample output

```
dig @${coredns1IP} k.example.com +subnet=127.0.200.200 +short
klb.k.example.com.
geo-na.klb.k.example.com.
1fopkr-2y5vu7.klb.k.example.com.
10.89.0.19

dig @${coredns2IP} k.example.com +subnet=127.0.200.200 +short
klb.k.example.com.
geo-na.klb.k.example.com.
1fopkr-2y5vu7.klb.k.example.com.
10.89.0.19
```

### Test Weighted routing

As we only have 2 core dns instances on the single cluster, we will need to modify one of the existing DNSPolicies so that both gateways are considered to be in the same Geo:

```
kubectl patch dnspolicy dnspolicy-na -n kuadrant-coredns-1 --type='json' -p='[{"op": "replace", "path": "/spec/loadBalancing/geo","value": "GEO-EU"},{"op": "replace", "path": "/spec/loadBalancing/defaultGeo", "value": true},{"op": "replace", "path": "/spec/loadBalancing/weight", "value": 100 }]'
```

Next we will re-run our DNS queries:

```
for i in {1..20}; do dig @${coredns2IP} k.example.com +subnet=127.0.100.100  +short; sleep 1;  done
for i in {1..20}; do dig @${coredns1IP} k.example.com +subnet=127.0.100.100  +short; sleep 1;  done

```

Again you should see a similar response from both nameservers with the IPAddresses alternating (although due to cache it may not be a perfect 50/50 split)

example response:

```
klb.k.example.com.
geo-eu.klb.k.example.com.
15u8qo-2y5vu7.klb.k.example.com.
10.89.0.19
klb.k.example.com.
geo-eu.klb.k.example.com.
15u8qo-2y5vu7.klb.k.example.com.
10.89.0.19
klb.k.example.com.
geo-eu.klb.k.example.com.
15u8qo-332u3t.klb.k.example.com.
10.89.0.20
```