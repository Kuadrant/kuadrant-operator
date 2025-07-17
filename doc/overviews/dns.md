# Kuadrant DNS

A Kuadrant DNSPolicy custom resource:

Targets Gateway API networking resources [Gateways](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.Gateway) to provide dns management by managing the lifecycle of dns records in external dns providers such as AWS Route53 and Google DNS.

## How it works

A DNSPolicy and its targeted Gateway API networking resource contain all the statements to configure both the ingress gateway and the external DNS service.
The needed dns names are gathered from the listener definitions and the IPAdresses | CNAME hosts are gathered from the status block of the gateway resource.

### The DNSPolicy custom resource

#### Overview

The `DNSPolicy` spec includes the following parts:

- A reference to an existing Gateway API resource (`spec.targetRef`)
- LoadBalancing specification (`spec.loadBalancing`)
- HealthCheck specification (`spec.healthCheck`)

Check out the [API reference](../reference/dnspolicy.md) for a full specification of the DNSPolicy CRD.

## Using the DNSPolicy

### DNS Provider Setup

A DNSPolicy acts against a target Gateway by processing its listeners for hostnames that it can create dns records for.
In order for it to do this, it must know about the dns provider.
This is done through the creation of dns provider secrets containing the credentials and configuration for the dns provider account.

The policy can use a specific provider secret by referencing it in the `ProviderRefs`. Alternatively, if no reference is provided, the secret with `kuadrant.io/default-provider=true` label will be chosen as a default option.

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
apiVersion: kuadrant.io/v1
kind: DNSPolicy
metadata:
  name: <DNSPolicy name>
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: <Gateway Name>
```

### Targeting a specific Listener of a gateway

A DNSPolicy can target a specific listener in a gateway using the `sectionName` property of the targetRef configuration. When you set the `sectionName`, the DNSPolicy will only affect that listener and no others. If you also have another DNSPolicy targeting the entire gateway, the more specific policy targeting the listerner will be the policy that is applied.

```yaml
apiVersion: kuadrant.io/v1
kind: DNSPolicy
metadata:
  name: <DNSPolicy name>
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: <Gateway Name>
    sectionName: <myListenerName>
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

* One Gateway can only be targeted by one DNSPolicy unless subsequent DNSPolicies choose to specific a sectionName in their targetRef.
* DNSPolicies can only target Gateways defined within the same namespace of the DNSPolicy.

## Troubleshooting 
### Understanding status
The `Status.Conditions` on DNSPolicy mostly serves as an aggregation of the DNSRecords conditions. 
The DNSPolicy conditions: 
- `Accepted` indicates that policy was validated and is accepted by the controller for the reconciliation. 
- `Enforced` indicates that the controller acted upon the policy. If DNSRecords were created as the result this condition will reflect the `Ready` condition on the record. This condition is removed if `Accepted` is false. If partially enforced, the condition will be set to `True`
- `SubResourcesHealthy` reflects `Healthy` conditions of sub-resources. This condition is removed if `Accepted` is false. If partially healthy, the condition will be set to `False` 

The `Status.Conditions` on the DNSRecord are as follows: 
- `Ready` indicates that the record was successfully published to the provider. 
- `Healthy` indicates that dnshealthcheckprobes are healthy. If not all probes are healthy, the condition will be set to `False`



### Logs 
To increase the log level of the `kuadran-operator` refer to [this](https://github.com/Kuadrant/kuadrant-operator/blob/main/doc/overviews/logging.md) logging doc.

To increase the log level of the `dns-operator-controller-manager` and for the examples on log queries refer to the [logging](https://github.com/Kuadrant/dns-operator/blob/main/README.md#logging) section in the DNS Operator readme 

### Debugging
This section will provide the typical sequence of actions during the troubleshooting. 
It is meant to be a reference to identifying the problem rather than SOP. 
#### List policies to identify the failing one 
```shell
kubectl get dnspolicy -A -o wide
```
#### Inspect the failing policy 
```shell
kubectl get dnspolicy <dnspolicy-name> -n <dnspolicy-namespace> -o yaml | yq '.status.conditions'
```
The output will show which DNSRecords and for what reasons are failing. For example: 
```
- lastTransitionTime: "2024-12-04T09:46:22Z"
  message: DNSPolicy has been accepted
  reason: Accepted
  status: "True"
  type: Accepted
- lastTransitionTime: "2024-12-04T09:46:29Z"
  message: 'DNSPolicy has been partially enforced. Not ready DNSRecords are: test-api '
  reason: Enforced
  status: "True"
  type: Enforced
- lastTransitionTime: "2024-12-04T09:46:27Z"
  message: 'DNSPolicy has encountered some issues: not all sub-resources of policy are passing the policy defined health check. Not healthy DNSRecords are: test-api '
  reason: Unknown
  status: "False"
  type: SubResourcesHealthy
```
This example indicates that the policy was accepted and one of the DNSRecords - `test-api` DNSRecord - is not ready and not healthy 

#### Locate sub-records to confirm conditions
This ensures that the Kuadrand operator propagated status correctly. The names of the DNSRecords are composed of the Gateway name followed by a listener name and are created in the DNSPolicy namespace.
```shell
kubectl get dnsrecord -n <dnspolicy-namespace> 
```

#### Inspect the record to get more detailed information on the failure
```shell
kubectl get dnsrecord <dnsrecord-name> -n <dnspolicy-namespace> -o yaml | yq '.status'
```
Most of the time the `conditions` will hold all necessary information. 
However, it is advised to pay attention to the `queuedAt` and `validFor` field 
to understand when the record was processed and when controller expects it to be reconciled again. 

#### Inspect health check probes 
We create a probe per address per dns record. The name of the probe is DNSRecord name followed by an address. 
```shell
# list probes 
kubectl get dnshealthcheckprobe -n <dnspolicy-namespace>
# inspect the probe 
kubectl get dnshealthcheckprobe <probe-name> -n <dnspolicy-namespace> -o yaml | yq '.status'
```
#### Identify what in logs to look for 
There are two operators to look into and a number of controllers.
The commands above should provide an understanding of what component/process is failing. 
Use the following to identify the correct controller:
- If the problem in the status propagation from the DNSRecord to the DNSPolicy or in the creation of the DNSRecord: `kuadrant-operator` logs under `kuadrant-operator.EffectiveDNSPoliciesReconciler` reconciler
- If the problem is in publishing DNSRecord or reacting to the healtcheckprobe CR: `dns-operator-controller-manager` logs under `dnsrecord_controller` reconciler
- If the problem in creation of the probes: `dns-operator-controller-manager` logs under `dnsrecord_controller.healthchecks` reconciler
- If the problem is in the execution of the healthchecks: `dns-operator-controller-manager` logs under `dnsprobe_controller` reconciler
