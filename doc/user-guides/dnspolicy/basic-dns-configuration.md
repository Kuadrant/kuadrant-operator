## Basic DNS setup

The document will cover the most basic DNS setup using the Kuadrant [DNSPolicy](https://docs.kuadrant.io/0.11.0/kuadrant-operator/doc/reference/dnspolicy/) API. In order to follow this guide, it is expected that you have a cluster setup with the latest version of Kuadrant installed. Also as we are using DNS, it is also important that the Gateways are accessible either via your local network or via the public internet. DNSPolicy will work with any Gateway provider so it is not essential that you have Istio or Envoy Gateway installed, but you do need a [Gateway API provider](https://gateway-api.sigs.k8s.io/implementations/) installed. We would recommend using Istio or Envoy Gateway as this will allow you to use some of the other policies provided by Kuadrant.


### Gateway and HTTPRoute configuration

With a Gateway provider installed, in order to configure DNS via `DNSPolicy`, you must first configure a Gateway with a listener that uses a specified hostname. You must also have a HTTPRoute resource attached to this gateway listener. Below are some simple examples of these resources (note we are not using a HTTPS listener for simplicity but that will also work):

```yaml
---
kind: Gateway
apiVersion: gateway.networking.k8s.io/v1
metadata:
  name: external
spec:
  gatewayClassName: istio
  listeners:
    - name: http
      port: 8080
      hostname: test.example.com
      protocol: HTTP
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: toystore
  labels:
    app: toystore
spec:
  parentRefs:
    - name: external
  hostnames: ["test.example.com"]
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: "/toy"
          method: GET
        - path:
            type: Exact
            value: "/admin/toy"
          method: POST
        - path:
            type: Exact
            value: "/admin/toy"
          method: DELETE
      backendRefs:
        - name: toystore
          port: 80
```      
With these defined, we are ready to setup DNS via DNSPolicy.

### Configure a DNSProvider

The first step is to configure a DNSProvider. This is a simple kubernetes secret with credentials to access the DNS provider. With Kuadrant we support using `AWS Route53, Azure and GCP` as DNS providers. It is important that this credential has access to write and read to your DNS zones.

More info on the various [DNS Providers](https://github.com/Kuadrant/dns-operator/blob/main/docs/provider.md)

In this example we will configure an AWS route53 DNS provider:

```
kubectl create secret generic aws-credentials \
  --namespace=my-gateway-namespace \
  --type=kuadrant.io/aws \
  --from-literal=AWS_ACCESS_KEY_ID=XXXX \
  --from-literal=AWS_REGION=eu-west-1 \
  --from-literal=AWS_SECRET_ACCESS_KEY=XXX

```

With this in place we can now define our DNSPolicy resource:

```yaml
apiVersion: kuadrant.io/v1
kind: DNSPolicy
metadata:
  name: basic-dnspolicy
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: external
  providerRefs:
    - name: aws-credentials
```

This resource also needs to be created in the same namespace as your Gateway and the `targetRef` needs to reference your gateway. When this is done we can check the status of the DNSPolicy and the Gateway to check when it is ready.

```
kubectl wait dnspolicy/basic-dnspolicy -n my-gateway-namespace --for="condition=Ready=true" --timeout=300s

```

If you look at the gateway status you should also see:

```
  - lastTransitionTime: "2024-10-09T11:22:10Z"
    message: Object affected by DNSPolicy kuadrant-system/simple-dnspolicy
    observedGeneration: 1
    reason: Accepted
    status: "True"
    type: kuadrant.io/DNSPolicyAffected
``` 

DNS is now setup for your Gateway. After allowing a little time for the DNS propagate to the nameservers, you should be able to test the DNS using a dig command alternatively you can curl your endpoint.

```
dig test.example.com +short

curl -v test.example.com/toy

```

### Important Considerations

With this guide, you have learned how to setup the most basic DNSPolicy. DNSPolicy is also capable of setting up advanced DNS record structure to help balance traffic across multiple gateways. With the most basic policy outlined here, you should not apply it to more than one gateway that shares a listener with the same host name. There is one exception to this rule, which is if all your gateways are using IP addresses rather than hostname addresses; in this case DNSPolicy will merge the IPs into a multi-value response. However, if your Gateways are using hostnames, DNSPolicy will set up a simple CNAME record and as there is only one record and CNAMEs cannot have multiple values by definition, one of the DNSPolicies (the last one to attempt to update the provider) will report an error. 

