# DNS Health Checks

The DNS health check feature allows you to define a HTTP based health check via the DNSPolicy API that will be executed against targeted gateway listener(s) that have specified **none** wildcard hostnames. These health checks will flag a published endpoint as healthy or unhealthy based on the defined configuration. When unhealthy an endpoint will not be published if it has **not** already been published to the DNS provider, will **only** be unpublished if it is part of a multi-value A record and in all cases can be observable via the DNSPolicy status.

## Limitations

- We do not currently support a health check being targeted to a `HTTPRoute` resource: DNSPolicy can only target Gateways. 
- As mentioned above, when a record has been published using the load balancing options (GEO and Weighting) via DNSPolicy, a failing health check will not remove the endpoint record from the provider, this is to avoid an accidental NX-Domain response. If the policy is not using the load balancing options and results in a multiple value A record, then unhealthy IPs will be removed from this A record unless it would result in an empty value set. 
- Health checks will not be added to listeners that define a wildcard hostname E.G (*.example.com) as we currently cannot know which host to use to for the health check.


## Configuration of Health Checks

To configure a DNS health check, you need to specify the `health check` section of the [DNSPolicy](https://docs.kuadrant.io/latest/kuadrant-operator/doc/reference/dnspolicy/#healthcheckspec).


Below are some examples of DNSPolicy with health checks defined:


1) DNSPolicy with a health check that will be applied to all listeners on a gateway that define a none wildcard hostname

```yaml
apiVersion: kuadrant.io/v1
kind: DNSPolicy
metadata:
  name: gateway-dns
spec:
  healthCheck:
    failureThreshold: 3
    interval: 5m
    path: /health
  ...
   targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: external  
```


2) DNSPolicy with health check  that will be applied for a specific listener with a none wildcard hostname

```yaml
apiVersion: kuadrant.io/v1
kind: DNSPolicy
metadata:
  name: my-listener-dns
spec:
  healthCheck:
    failureThreshold: 3
    interval: 5m
    path: /ok #different path for this listener
  ...
   targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: external  
    sectionName: my-listener #notice the addition of section name here that must match the listener name
```

These policies can be combined on a single gateway. The policy with the section name defined will override the gateway policy including the health check.

## Sending additional headers with the health check request


Sometimes, it may be desirable to send some additional headers with the health check request. For example to send API key or service account token that can be defined in the request headers.

To do this you will need to create a secret in the same namespace as the DNSPolicy with the keys and values you wish to send:

```bash
kubectl create secret generic healthheaders --from-literal=token=supersecret -n my-dns-policy-namespace
```

Next you will need to update the DNSPolicy to add a reference to this secret:


```yaml
apiVersion: kuadrant.io/v1
kind: DNSPolicy
metadata:
  name: my-listener-dns
spec:
  healthCheck:
    additionalHeadersRef: #add the following
      name: healthheaders
    failureThreshold: 3
    interval: 5m
    path: /ok
  ...
   targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: external  
    sectionName: my-listener
```

The health check requests will now send the key value pairs in the secret as headers when performing a health check request.

## Health Check Status


When all health checks based on a DNSPolicy are passing you will see the following status:

```yaml
    - lastTransitionTime: "2024-11-14T12:33:13Z"
      message: All sub-resources are healthy
      reason: SubResourcesHealthy
      status: "True"
      type: SubResourcesHealthy
```

If one or more of the health checks are failing you will see a status in the DNSPolicy simiar to the one shown below:

```yaml
   - lastTransitionTime: "2024-11-15T10:40:15Z"
      message: 'DNSPolicy has encountered some issues: not all sub-resources of policy
        are passing the policy defined health check. Not healthy DNSRecords are: external-t1b '
      reason: Unknown
      status: "False"
      type: SubResourcesHealthy
    observedGeneration: 1
    recordConditions:
      t1b.cb.hcpapps.net:
      - lastTransitionTime: "2024-11-15T10:40:14Z"
        message: 'Not healthy addresses: [aeeba26642f1b47d9816297143e2d260-434484576.eu-west-1.elb.amazonaws.com]'
        observedGeneration: 1
        reason: health checksFailed
        status: "False"
        type: Healthy
```        

Finally, you can also take a look at the underlying individual health check status by inspecting the `dnshealthcheckprobe` resource:

>**Note**: These resources are for view only interactions as they are controlled by the Kuadrant Operator based on the DNSPolicy API

```bash
kubectl get dnshealthcheckprobes n my-dns-policy-namespace -o=wide
```

If you look at the status of one of these you can see additional information:

```yaml
status:
  consecutiveFailures: 3
  healthy: false
  observedGeneration: 1
  reason: 'Status code: 503'
  status: 503
```

## Manually removing unhealthy records

If you have a failing health check for one of your gateway listeners and you would like to remove it from the DNS provider, you can do this by deleting the associated DNSRecord resource.

**Finding the correct record**

DNSRecord resources are kept in the same namespace as the DNSPolicy that configured and created them.

```bash
kubectl get dnsrecords.kuadrant.io -n <dns-policy-namespace>
```

As shown above, when a health check is failing, the DNSPolicy will show a status for that listener host to surface that failure:

```yaml
recordConditions:
    t1a.cb.hcpapps.net:
    - lastTransitionTime: "2024-11-27T14:00:52Z"
      message: 'Not healthy addresses: [ae4d131ee5d7b4fb098f4afabf4aba4c-513237325.us-east-1.elb.amazonaws.com]'
      observedGeneration: 1
      reason: HealthChecksFailed
      status: "False"
      type: Healthy
```   

The DNSRecord resource is named after the gateway and the listener name. So if you have a gateway called `ingress` and a listener called `example` you will have a `DNSRecord` resource named `ingress-example` in the same namespace as your DNSPolicy. So from this status you can get the hostname and find the associated listener on your gateway. You can then delete the associated DNSRecord resource. 

```bash
kubectl delete dnsrecord.kuadrant.io <gateway-name>-<listener-name> -n <dns policy namespace>
```

Removing this resource will remove all of the associated DNS records in the DNS provider and while the health check is failing, the dns operator will not re-publish these records. 
