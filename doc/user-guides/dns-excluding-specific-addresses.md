## Excluding specific addresses from being published

By default DNSPolicy takes all the addresses published in the status of the Gateway it is targeting and use these values in the DNSRecord it publishes to chosen DNS provider. 

There could be cases where you have an address assigned to a gateway that you do not want to publish to a DNS provider, but you still want DNSPolicy to publish records for other addresses.

To prevent a gateway address being published to the DNS provider, you can set the `excludeAddresses` field in the DNSPolicy resource targeting the gateway. The `excludeAddresses` field can be set to a hostname, an IPAddress or a CIDR.

Below is an example of a DNSPolicy excluding a hostname:

```
apiVersion: kuadrant.io/v1alpha1
kind: DNSPolicy
metadata:
  name: prod-web
  namespace: ${DNSPOLICY_NAMESPACE}
spec:
  targetRef:
    name: prod-web-istio
    group: gateway.networking.k8s.io
    kind: Gateway
  providerRefs:
    - name: aws-credentials
  loadBalancing:
    weight: 120
    geo: EU
    defaultGeo: true
  excludeAddresses:
    - "some.local.domain"
```

In the above case `some.local.domain` will not be set up as a CNAME record in the DNS provider.

**Note**: It is valid to exclude all addresses. However this will result in existing records being removed and no new ones being created.
