# Kuadrant DNS

A Kuadrant DNSPolicy custom resource:

1. Targets Gateway API networking resources [Gateways](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.Gateway) to provide dns management by managing the lifecycle of dns records in external dns providers such as AWS Route53 and Google DNS.

## How it works

A DNSPolicy and its targeted Gateway API networking resource contain all the statements to configure both the ingress gateway and the external DNS service. 
The needed dns names are gathered from the listener definitions and the IPAdresses | CNAME hosts are gathered from the status block of the gateway resource.

### The DNSPolicy custom resource

#### Overview

The `DNSPolicy` spec includes the following parts:

* A reference to an existing Gateway API resource (`spec.targetRef`)
* LoadBalancing specification (`spec.loadBalancing`)
* HealthCheck specification (`spec.healthCheck`)

#### High-level example and field definition

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: DNSPolicy
metadata:
  name: my-dns-policy
spec:
  # reference to an existing networking resource to attach the policy to
  # it can only be a Gateway API Gateway resource
  # it can only refer to objects in the same namespace as the DNSPolicy
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: mygateway
    
 # reference to an existing secret resource containing provider credentials and configuration
 # it can only refer to Secrets in the same namespace as the DNSPolicy that have the type kuadrant.io/(provider) e.g kuadrant.io/aws
  providerRefs:
   - name: my-aws-credentials
 
  # (optional) loadbalancing specification
  # use it for providing the specification of how dns will be configured in order to provide balancing of load across multiple clusters
  loadBalancing:
    # default geo to be applied to records
    defaultGeo: true
    # weighted specification
    weight: 100
    # 
    geo: IE

  # (optional) health check specification
  # health check probes with the following specification will be created for each DNS target
  healthCheck:
    allowInsecureCertificates: true
    endpoint: /
    expectedResponses:
      - 200
      - 201
      - 301
    failureThreshold: 5
    port: 443
    protocol: https
```

Check out the [API reference](reference/dnspolicy.md) for a full specification of the DNSPolicy CRD.

## Using the DNSPolicy

### DNS Provider Setup

A DNSPolicy acts against a target Gateway by processing its listeners for hostnames that it can create dns records for. 
In order for it to do this, it must know about the dns provider.
This is done through the creation of dns provider secrets containing the credentials and configuration for the dns provider account.

If for example a Gateway is created with a listener with a hostname of `echo.apps.hcpapps.net`:
```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: my-gw
spec:
  listeners:
    - allowedRoutes:
        namespaces:
          from: All
      name: api
      hostname: echo.apps.hcpapps.net
      port: 80
      protocol: HTTP
```

In order for the DNSPolicy to act upon that listener, a DNS provider Secret must exist for that hostnames' domain.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-aws-credentials
  namespace: <Gateway Namespace>
data:
  AWS_ACCESS_KEY_ID: <AWS_ACCESS_KEY_ID>
  AWS_REGION: <AWS_REGION>
  AWS_SECRET_ACCESS_KEY: <AWS_SECRET_ACCESS_KEY>
type: kuadrant.io/aws
```

By default, Kuadrant will list the available zones and find the matching zone based on the listener host in the gateway listener. If it finds more than one matching zone for a given listener host, it will not update any of those zones. 
When providing a credential you should limit that credential down to just have write access to the zones you want Kuadrant to manage. Below is an example of a an AWS policy for doing this type of thing:

```
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Sid": "VisualEditor0",
            "Effect": "Allow",
            "Action": [
                "route53:ListTagsForResources",
                "route53:GetHealthCheckLastFailureReason",
                "route53:GetHealthCheckStatus",
                "route53:GetChange",
                "route53:GetHostedZone",
                "route53:ChangeResourceRecordSets",
                "route53:ListResourceRecordSets",
                "route53:GetHealthCheck",
                "route53:UpdateHostedZoneComment",
                "route53:UpdateHealthCheck",
                "route53:CreateHealthCheck",
                "route53:DeleteHealthCheck",
                "route53:ListTagsForResource",
                "route53:ListHealthChecks",
                "route53:GetGeoLocation",
                "route53:ListGeoLocations",
                "route53:ListHostedZonesByName",
                "route53:GetHealthCheckCount"
            ],
            "Resource": [
                "arn:aws:route53:::hostedzone/Z08187901Y93585DDGM6K",
                "arn:aws:route53:::healthcheck/*",
                "arn:aws:route53:::change/*"
            ]
        },
        {
            "Sid": "VisualEditor1",
            "Effect": "Allow",
            "Action": [
                "route53:ListHostedZones"
            ],
            "Resource": "*"
        }
    ]
}
```


### Targeting a Gateway networking resource

When a DNSPolicy targets a Gateway, the policy will be enforced on all gateway listeners.

Target a Gateway by setting the `spec.targetRef` field of the DNSPolicy as follows:

```yaml
apiVersion: kuadrant.io/v1beta2
kind: DNSPolicy
metadata:
  name: <DNSPolicy name>
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: <Gateway Name>
```

### DNSRecord Resource

The DNSPolicy will create a DNSRecord resource for each listener hostname. The DNSPolicy resource uses the status of the Gateway to determine what dns records need to be created based on the clusters it has been placed onto.

Given the following multi cluster gateway status:
```yaml
status:
  addresses:
    - type: kuadrant.io/MultiClusterIPAddress
      value: kind-mgc-workload-1/172.31.201.1
    - type: kuadrant.io/MultiClusterIPAddress
      value: kind-mgc-workload-2/172.31.202.1
  listeners:
    - attachedRoutes: 1
      conditions: []
      name: kind-mgc-workload-1.api
      supportedKinds: []
    - attachedRoutes: 1
      conditions: []
      name: kind-mgc-workload-2.api
      supportedKinds: []        
```

A DNSPolicy targeting this gateway would create an appropriate DNSRecord based on the routing strategy selected.

#### loadbalanced
```yaml
apiVersion: kuadrant.io/v1alpha1
kind: DNSRecord
metadata:
  name: echo.apps.hcpapps.net
  namespace: <Gateway Namespace>
spec:
  endpoints:
    - dnsName: 24osuu.lb-2903yb.echo.apps.hcpapps.net
      recordTTL: 60
      recordType: A
      targets:
        - 172.31.202.1
    - dnsName: default.lb-2903yb.echo.apps.hcpapps.net
      providerSpecific:
        - name: weight
          value: "120"
      recordTTL: 60
      recordType: CNAME
      setIdentifier: 24osuu.lb-2903yb.echo.apps.hcpapps.net
      targets:
        - 24osuu.lb-2903yb.echo.apps.hcpapps.net
    - dnsName: default.lb-2903yb.echo.apps.hcpapps.net
      providerSpecific:
        - name: weight
          value: "120"
      recordTTL: 60
      recordType: CNAME
      setIdentifier: lrnse3.lb-2903yb.echo.apps.hcpapps.net
      targets:
        - lrnse3.lb-2903yb.echo.apps.hcpapps.net
    - dnsName: echo.apps.hcpapps.net
      recordTTL: 300
      recordType: CNAME
      targets:
        - lb-2903yb.echo.apps.hcpapps.net
    - dnsName: lb-2903yb.echo.apps.hcpapps.net
      providerSpecific:
        - name: geo-country-code
          value: '*'
      recordTTL: 300
      recordType: CNAME
      setIdentifier: default
      targets:
        - default.lb-2903yb.echo.apps.hcpapps.net
    - dnsName: lrnse3.lb-2903yb.echo.apps.hcpapps.net
      recordTTL: 60
      recordType: A
      targets:
        - 172.31.201.1
  providerRefs:
    - name: my-aws-credentials
```

After DNSRecord reconciliation the listener hostname should be resolvable through dns:

```bash
dig echo.apps.hcpapps.net +short
lb-2903yb.echo.apps.hcpapps.net.
default.lb-2903yb.echo.apps.hcpapps.net.
lrnse3.lb-2903yb.echo.apps.hcpapps.net.
172.31.201.1
```

#### simple
```yaml
apiVersion: kuadrant.io/v1alpha1
kind: DNSRecord
metadata:
  name: echo.apps.hcpapps.net
  namespace: <Gateway Namespace>
spec:
  endpoints:
    - dnsName: echo.apps.hcpapps.net
      recordTTL: 60
      recordType: A
      targets:
        - 172.31.201.1
        - 172.31.202.1
  providerRefs:
   - name: my-aws-credentials 
```

After DNSRecord reconciliation the listener hostname should be resolvable through dns:

```bash
dig echo.apps.hcpapps.net +short
172.31.201.1
```

### Known limitations

* One Gateway can only be targeted by one DNSPolicy.
* DNSPolicies can only target Gateways defined within the same namespace of the DNSPolicy.
