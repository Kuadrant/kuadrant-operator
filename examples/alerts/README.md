## SLO Multi burn rate multi window alerts
Kuadrant have created two example SLO alerts to help give ideas on the types of SLO alerts that could be used with the operator. We have created one alert for latency and one for availability, both are Multiwindow, Multi-Burn-Rate Alerts. The alerts show a scenario where a 28d rolling window is used and a uptime of 99.95% i.e only 0.05% error budget margin is desired. This in real world time would be downtime of around: 

| Time Frame | Duration   |
|------------|------------|
| Daily:     | 43s        |
| Weekly:    | 5m 2.4s    |
| Monthly:   | 21m 44s    |
| Quarterly: | 1h 5m 12s  |
| Yearly:    | 4h 20m 49s |

These values can be changed to suit different scenarios

### Sloth
Sloth is a tool to aid in the creation of multi burn rate and multi window SLO alerts and was used to create both the availability and latency alerts. It follows the common standard set out by [Google's SRE book](https://sre.google/workbook/implementing-slos/). Sloth generates alerts based on specific specs given. The specs for our example alerts can be found in the example/sloth folder.

#### Metrics used for the alerts
 
#### Availability
For the availability SLO alerts the Istio metric `istio_requests_total` was used as its a counter type metric meaning the values can only increase as well as it gives information on all requests handled by the Istio proxy.

#### Latency
For the availability SLO alerts the Istio metric `istio_request_duration_milliseconds` was used as its a [Histogram](https://prometheus.io/docs/concepts/metric_types/#histogram).

### Sloth generation
You can modify the examples Sloth specs we have and regenerate the prometheus rules using the Sloth CLI and the generate command. For more information please the [Sloth website](https://sloth.dev/usage/cli/)

```
sloth generate -i examples/alerts/sloth/latency.yaml --default-slo-period=28d
```
You can also use the make target to generate the rules to.

```
make sloth-generate
```
### Prometheus unit tests
There are also two matching unit tests to verify and test the alerts that Sloth has generated. These can be run using the make target:

```
make alerts-tests
```
