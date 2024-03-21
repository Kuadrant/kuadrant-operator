# Metrics

This is a reference page for some of the different metrics used in example
dashboards and alerts. It is not an exhaustive list. The documentation for each
component may provide more details on a per-component basis. Some of the metrics
are sourced from components outside the Kuadrant project, for example, Envoy.
The value of this reference is showing some of the more widely desired metrics,
and how to join the metrics from different sources together in a meaningful way.

## Metrics sources

* Kuadrant components
* [Istio](https://istio.io/latest/docs/reference/config/metrics/)
* [Envoy](https://www.envoyproxy.io/docs/envoy/latest/operations/admin.html#get--stats)
* [Kube State Metrics](https://github.com/kubernetes/kube-state-metrics)
* [Gateway API State Metrics](https://github.com/Kuadrant/gateway-api-state-metrics)
* [Kubernetes metrics](https://kubernetes.io/docs/concepts/cluster-administration/system-metrics/#metrics-in-kubernetes)

## Resource usage metrics

Resource metrics, like cpu, memory and disk usage, primarily come from the Kubernetes
metrics components. These include `container_cpu_usage_seconds_total`, `container_memory_working_set_bytes`
and `kubelet_volume_stats_used_bytes`. A [stable list of metrics](https://github.com/kubernetes/kubernetes/blob/master/test/instrumentation/testdata/stable-metrics-list.yaml) is maintained in
the kubernetes repository. These low level metrics typically have a set of
[recording rules](https://prometheus.io/docs/practices/rules/#aggregation) that
aggregate values by labels and time ranges.

## Networking metrics

Low level networking metrics like `container_network_receive_bytes_total` are also
available from the Kubernetes metrics components.
HTTP & GRPC traffic metrics with higher level labels are [available from Istio](https://istio.io/latest/docs/reference/config/metrics/).
One of the main metrics would be `istio_requests_total`.

## State metrics

The [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics/tree/main/docs#default-resources)
project exposes the state of various kuberenetes resources
as metrics and labels. For example, the ready `status` of a `Pod` is available as
`kube_pod_status_ready`, with labels for the pod `name` and `namespace`. This can
be useful for linking lower level container metrics back to a meaningful resource
in the kubernetes world.

## Joining metrics

Metric queries can be as simple as just the name of the metric, or can be complex
with joining & grouping. A lot of the time it can be useful to tie back low level
metrics to more meaningful kubernetes resources. For example, if the memory usage
is maxed out on a container and that container is constantly being OOMKilled, it
can be useful to get the Deployment and Namespace of that container for debugging.
Prometheus query language (or promql) allows [vector matching](https://prometheus.io/docs/prometheus/latest/querying/operators/#vector-matching)
or results (sometimes called joining).

When using Gateway API and Kuadrant resources like HTTPRoute and RateLimitPolicy,
the state metrics can be joined to istio metrics to give a meaningful result set.
Here's an example that queries the number of requests per second, and includes
the name of the HTTPRoute that the traffic is for.

```promql
sum(
    rate(
        istio_requests_total{}[5m]
    )
) by (destination_workload)

* on(destination_workload) group_right 
    label_replace(gatewayapi_httproute_labels{}, \"destination_workload\", \"$1\",\"deployment\", \"(.+)\")
```

Breaking this query down, there are 2 parts.
The first part is getting the rate of requests hitting the istio gateway, aggregated
to 5m intervals:

```promql
sum(
    rate(
        istio_requests_total{}[5m]
    )
) by (destination_workload)
```

The result set here will include a label for the destination workload name (i.e.
the Service in Kubernetes). This label is key to looking up the HTTPRoute this
traffic belongs to.

The 2nd part of the query uses the `gatewayapi_httproute_labels` metric and the
`label_replace` function. The `gatewayapi_httproute_labels` metric gives a list
of all httproutes, including any labels on them. The HTTPRoute in this example
has a label called 'deployment', set to be the same as the istio workload name.
This allows us to join the 2 results set.
However, because the label doesn't match exactly (`destination_workload` and `deployment`),
we can replace the label so that it does match. That's what the `label_replace`
does.

```promql
    label_replace(gatewayapi_httproute_labels{}, \"destination_workload\", \"$1\",\"deployment\", \"(.+)\")
```

The 2 parts are joined together using vector matching.

```promql
* on(destination_workload) group_right 
```

* `*` is the binary operator i.e. multiplication (gives join like behaviour)
* `on()` specifies which labels to "join" the 2 results with
* `group_right` enables a one to many matching.

See the [prometheus documentation](https://prometheus.io/docs/prometheus/latest/querying/operators/#vector-matching) for further details on matching.