# DNS Health Checks
DNS Health Checks are a crucial tool for ensuring the availability and reliability of your multi-cluster applications. Kuadrant offers a powerful feature known as DNSPolicy, which allows you to configure and verify health checks for DNS endpoints. This guide provides a comprehensive overview of how to set up, utilize, and understand DNS health checks.

## What are DNS Health Checks?
DNS Health Checks are a way to assess the availability and health of DNS endpoints associated with your applications. These checks involve sending periodic requests to the specified endpoints to determine their responsiveness and health status. by configuring these checks via the [DNSPolicy](./dns.md), you can ensure that your applications are correctly registered, operational, and serving traffic as expected.

## Configuration of Health Checks
>Note: By default, health checks occur at 60-second intervals.

To configure a DNS health check, you need to specify the `healthCheck` section of the DNSPolicy. The key part of this configuration is the `healthCheck` section, which includes important properties such as:

* `allowInsecureCertificates`: Added for development environments, allows health probes to not fail when finding an invalid (e.g. self-signed) certificate.
* `additionalHeadersRef`: This refers to a secret that holds extra headers for the probe to send, often containing important elements like authentication tokens.
* `endpoint`: This is the path where the health checks take place, usually represented as '/healthz' or something similar.
* `expectedResponses`: This setting lets you specify the expected HTTP response codes. If you don't set this, the default values assumed are 200 and 201.
* `failureThreshold`: It's the number of times the health check can fail for the endpoint before it's marked as unhealthy.
* `interval`: This property allows you to specify the time interval between consecutive health checks. The minimum allowed value is 5 seconds.
* `port`: Specific port for the connection to be checked.
* `protocol`: Type of protocol being used, like HTTP or HTTPS. **(Required)**


```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: DNSPolicy
metadata:
  name: prod-web
  namespace: multi-cluster-gateways
spec:
  targetRef:
    name: prod-web
    group: gateway.networking.k8s.io
    kind: Gateway
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
EOF
```
This configuration sets up a DNS health check by creating DNSHealthCheckProbes for the specified `prod-web` Gateway endpoints.

### `additionalHeadersRef`

The `additionalHeadersRef` field specifies a `Secret` used for storing supplementary HTTP headers. These headers are included when sending probe requests and can contain critical information like authentication tokens. This `Secret` must be in the same namespace as the DNSPolicy.

#### Usage Example

Here's how to use `additionalHeadersRef` in a `DNSPolicy` resource:

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: DNSPolicy
metadata:
  name: prod-web
  namespace: multi-cluster-gateways
spec:
  targetRef:
    name: prod-web
    group: gateway.networking.k8s.io
    kind: Gateway
  healthCheck:
    ...
    additionalHeadersRef:
      name: probe-headers
```

#### Creating a Secret for Additional Headers

To create a `Secret` that carries an additional header named "test-header" with the value "test," execute the following command:

```bash
kubectl create secret generic probe-headers \
  --namespace=multi-cluster-gateways \
  --from-literal=test-header=test
```

This will create a secret named `probe-headers` in the `multi-cluster-gateways` namespace, which can then be referenced in the `additionalHeadersRef` field of your `DNSPolicy`.

## How to Validate DNS Health Checks

After setting up DNS Health Checks to improve application reliability, it is important to verify their effectiveness. This guide provides a simple validation process to ensure that health checks are working properly and improving the operation of your applications.

1. Verify Configuration:
The first step in the validation process is to verify that the probes were created. Notice the label `kuadrant.io/gateway=prod-web` that only shows DNSHealthCheckProbes for the specified `prod-web` Gateway.
```
kubectl get -l kuadrant.io/gateway=prod-web dnshealthcheckprobes -A
```

2. Monitor Health Status:
The next step is to monitor the health status of the designated endpoints. This can be done by analyzing logs, metrics generated or by the health check probes status. By reviewing this data, you can confirm that endpoints are being actively monitored and that their status is being reported accurately.

The following metrics can be used to check all the attempts and failures for a listener.
```
mgc_dns_health_check_failures_total
mgc_dns_health_check_attempts_total
```

3. Test Failure Scenarios:
To gain a better understanding of how your system responds to failures, you can deliberately create endpoint failures. This can be done by stopping applications running on the endpoint or by blocking traffic, or for instance, deliberately omit specifying the expected 200 response code. This will allow you to see how DNS Health Checks dynamically redirect traffic to healthy endpoints and demonstrate their routing capabilities.

4. Monitor Recovery:
After inducing failures, it is important to monitor how your system recovers. Make sure that traffic is being redirected correctly and that applications are resuming normal operation.


## What Happens When a Health Check Fails
A pivotal aspect of DNS Health Checks is understanding of a health check failure. When a health check detects an endpoint as unhealthy, it triggers a series of strategic actions to mitigate potential disruptions:

1. The health check probe identifies an endpoint as "unhealthy" and it’s got greater consecutive failures than failure threshold.

2. The system reacts by immediately removing the unhealthy endpoint from the list of available endpoints, any endpoint that doesn’t have at least 1 healthy child will also be removed.

3. This removal causes traffic to automatically get redirected to the remaining healthy endpoints.

4. The health check continues monitoring the endpoint's status. If it becomes healthy again, endpoint is added to the list of available endpoints.

## Limitations

1. **Delayed Detection**: DNS health checks are not immediate; they depend on the check intervals. Immediate issues might not be detected promptly.

1. **No Wildcard Listeners**: Unsuitable for wildcard DNS listeners or dynamic domain resolution. DNS health checks do not cover wildcard listeners. Each endpoint must be explicitly defined.
