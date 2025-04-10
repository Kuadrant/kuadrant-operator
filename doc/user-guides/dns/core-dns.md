## CoreDNS with Kuadrant

With this guide, you will learn how to setup Kuadrant to use CoreDNS via the Kuadrant `DNSPolicy` and leverage CoreDNS as the authoritative nameserver(s) for a given domain shared across multiple gateways to provide both a weighted and GEO based DNS response similar to that offered by common cloud providers.


>Note: Core DNS support is intended for evaluation and feedback only. This guide makes use of a developer preview version of the CoreDNS integration and is **not** intended for production use.

The basic architecture for how the Core DNS integration works is shown in the image below:

![architecture](./core-dns.png)


### Summary

Kuadrant's DNS Operator will convert existing DNSRecords that reference a CoreDNS provider secret into two additional "CoreDNS" specific DNSRecords (named and labeled in a deterministic way). One of these will be the local record set with no weighting or geo provider specific data and will be served under a `kdrnt` TLD. The other record will be a merged record set of both the local `kdrnt` record and the `kdrnt` records of each other DNS nameserver for the same DNS name. The DNS Operator, via the provider secret, will be told about the set of nameservers it needs to query to form this single record set and will query these DNS nameservers directly and then merge their response into a single merged record set. This will mean each core dns will end up with the full record set for a given dns name.

Kuadrant's custom CoreDNS plugin, will read and serve these two new records (the merged record and the kdrnt record). If there is provider specific meta data for weight and GEO, the kuadrant plugin will apply GEO location filtering (assuming there is an appropriate GEO database configured) and a weighted response to any DNS queries for the dns names in those records.

It is important to note that no code is currently implemented in our CoreDNS integration to work with health checks.

### Environment for CoreDNS enabled

It is required to have at least one kubernetes cluster to use with CoreDNS. Either locally using kind, or using regular kubernetes clusters.

## Local environment with Kind
If you simply want to see how this works locally, and are not using this against existing clusters, the easiest approach is to use kind to create a local cluster, which we will use to simulate multiple clusters (using namespaced controllers). Here is a simple guide on how to do that (see image below). For this guide, we will have two instances of core dns.  

![local-setup](./local-setup.png)

To try this out, clone the kuadrant-operator repo first:

```
git clone https://github.com/Kuadrant/kuadrant-operator.git
```

From the root of the kuadrant-operator repo execute:

```
make local-setup && ./bin/kustomize build --enable-helm https://github.com/Kuadrant/dns-operator/config/coredns-multi | kubectl apply -f -
```

This will install Kuadrant into a local kind cluster and configure the CoreDNS instances. 

To show this working but keep the setup simple and in a single cluster, lets now setup 2 gateways on the same cluster that have listeners for the same hostname. Then use DNSPolicy to define one location as the EU and the other as NA.

As there are multiple instances of CoreDNS running and they are namespace scoped, our gateways and policies will be created in the same namespace as each of the dns servers. For convenience there are some gateways already defined.

> Note: these gateways use the k.example.com domain. This is pre-configured in the core dns corefile as a zone that uses the kuadrant CoreDns plugin.

```

kubectl apply -f examples/coredns/gateways.yaml

```

## A Kubernetes cluster

If you have existing clusters for which you want to have the DNS automated by the DNS Operator, you will need to install CoreDNS with the kuadrant plugin on those clusters, and configure them with a GEO IP database.

You will also need to have the [Kuadrant operator installed](https://artifacthub.io/packages/helm/kuadrant/kuadrant-operator) and running.

### Install CoreDNS
You can install CoreDNS configured with the kuadrant plugin using the follow kustomize command while kubectl is targeting the desired cluster, this will install CoreDNS into the `kuadrant-coredns` namespace:
```
kustomize build --enable-helm https://github.com/Kuadrant/dns-operator/config/coredns | kubectl apply -f -
```

For more information on CoreDNS installations, see [here](https://coredns.io/manual/installation/).

### Sample CoreDNS Configuration
The above installation will create a sample CoreDNS Config file, in the `kuadrant-coredns` namespace, named: `kuadrant-coredns`. This will need to be modified to suit your needs, if you are not running the local sample.

The sample CoreDNS configuration is generated from this file: [CoreDNS Configuration](https://raw.githubusercontent.com/Kuadrant/dns-operator/refs/heads/main/config/coredns/Corefile). That domain name can be changed or duplicated to add other domains as required.

#### Using a GEO IP database
The CoreDNS instances will need to be configured to use a GEO IP database, in the example above, this is called: `GeoLite2-City-demo.mmdb`. This is a mock database we provide to for illustrative purposes. Change this to refer to your maxmind database file, for more information, see [here](https://www.maxmind.com/en/geoip-databases).

#### Configuring your domain in CoreDNS
Generally, it should be sufficient to change the domain `k.example.com` in the above sample, to the domain you want the DNS Operator to manage, or duplicate it, to add multiple. For more information on configuring CoreDNS Please refer to their [documentation](https://coredns.io/manual/configuration/).

## Setup the DNSProvider secrets
To setup the DNSProvider secrets, you need to know the external IPAddress of each CoreDNS instance. This is to allow the DNS Operator to query each nameserver for its records to form a full record set for a given dns name.

### A Kubernetes cluster

After CoreDNS is installed and configured, look in the status of the CoreDNS service, for the ´loadBalancer` section which contains an ingress defining an IP. This is the IP of this CoreDNS nameserver.

Next you need to set the nameservers in the provider secret, as follows:
```
kubectl create secret generic core-dns --namespace=<COREDNS NAMESPACE> --type=kuadrant.io/coredns --from-literal=NAMESERVERS="<NAMESERVER>:53,<NAMESERVER>:53,..." --from-literal=ZONES="<DOMAIN IN COREDNS CONFIGURATION>,<DOMAIN IN COREDNS CONFIGURATION>,..."
```

### Local setup
In the local kind cluster, these commands will export the coreDNS IPs as envvars:
```
export coredns1IP=$(kubectl get service kuadrant-coredns-1 -n kuadrant-coredns-1 -o=jsonpath='{.status.loadBalancer.ingress[0].ip}')
export coredns2IP=$(kubectl get service kuadrant-coredns-2 -n kuadrant-coredns-2 -o=jsonpath='{.status.loadBalancer.ingress[0].ip}')
```

These will set up the 2 CoreDNS provider secrets:
```
kubectl create secret generic core-dns --namespace=kuadrant-coredns-1 --type=kuadrant.io/coredns --from-literal=NAMESERVERS="${coredns1IP}:53,${coredns2IP}:53" --from-literal=ZONES="k.example.com"

kubectl create secret generic core-dns --namespace=kuadrant-coredns-2 --type=kuadrant.io/coredns --from-literal=NAMESERVERS="${coredns2IP}:53,${coredns1IP}:53" --from-literal=ZONES="k.example.com"
```

## Enable CoreDNS in the DNS Operator

The CoreDNS provider is not enabled by default in the DNS Operator, as this is only in developer preview currently, to enable it in a normal Kuadrant installation, run the following command:

```
kubectl patch deployment dns-operator-controller-manager  --type='json' -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/args", "value": ["--metrics-bind-address=:8080", "--leader-elect","--provider=aws,google,azure,coredns"]}, {"op": "replace", "path": "/spec/template/spec/containers/0/image", "value": "quay.io/kuadrant/dns-operator:v0.14.0"}]'  -n kuadrant-system
```
## Delegate the zones (public cluster only)

This cannot be done when testing locally, but assuming the CoreDNS instance is running on a publically acessible IP which permits traffic on port 53, then the zone(s) in use will need to be delegated to these nameservers. For testing and evaluation we would recommend creating a fresh zone.

This is done by creating an NS record in the zone hosted by the authoritative nameserver. 

For example, if there is a domain example.com with authoritative nameservers in Route53, and 2 CoreDNS instances are configured with the zone k.example.com (on IPs 1.2.3.4 and 2.3.4.5). Then in the example.com zone in Route53 the following records need to be created:
```
coredns1.example.com. IN A 60 1.2.3.4
coredns2.example.com. IN A 60 2.3.4.5
k.example.com. IN NS 300 coredns1.example.com.
k.example.com. IN NS 300 coredns2.example.com.
```

If the CoreDNS instances are not publically accessible, then we will be able to verify them using the `@` modifer on a dig command.

### Setup a DNSPolicy

Now that you have your gateways and providers in place, you can move on to setup the DNSPolicies.

#### Running locally
If running locally, there is an example for this guide:

```
kubectl apply -f examples/coredns/dnspolicies.yaml
```

>Note:  2 DNSPolicies are created. In a real world scenario each of these gateways and policies would likely be on a different cluster. In the 2 DNSPolicies the main difference is the one targeting the NA gateway has a GEO set of NA while the one targeting the EU gateway has a GEO set of EU.

#### Kubernetes cluster

Otherwise if you are not following the local example, create the DNS Policy suitable for your cluster setup, some more information on the DNS Policy CR is available [here](https://docs.kuadrant.io/1.1.x/kuadrant-operator/doc/reference/dnspolicy/).

### Validate and test the setup

You can verify that everything is as expected by checking the DNSPolicy status. It is likely to take a minute or so before the status is marked as enforced but you should see `enforced true` for each policy.

```
kubectl get dnspolicy -A -o=wide
```

#### Local testing
Once they are all enforced. You can issue some queries against the local authoritative nameservers. 

```
dig @${coredns1IP} k.example.com
dig @${coredns2IP} k.example.com
```

#### Public cluster with delegated zone

```
dig k.example.com
```
