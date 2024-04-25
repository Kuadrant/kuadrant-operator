# Example Dashboards and Alerts

Explore a variety of starting points for monitoring your Kuadrant installation with our [examples](https://github.com/Kuadrant/kuadrant-operator/tree/main/examples) folder. These dashboards and alerts are ready-to-use and easily customizable to fit your environment.

## Dashboards

### Importing Dashboards into Grafana

- **UI Method:** Use the 'Import' feature in the Grafana UI to upload dashboard JSON files directly.
- **ConfigMap Method:** Automate dashboard provisioning by adding files to a ConfigMap, which should be mounted at `/etc/grafana/provisioning/dashboards`.

Datasources are configured as template variables, automatically integrating with your existing data sources. Metrics for these dashboards are sourced from [Prometheus](https://github.com/prometheus/prometheus). For more details on the metrics used, visit the [metrics](https://docs.kuadrant.io/kuadrant-operator/doc/observability/metrics/) documentation page.

## Alerts

### Setting Up Alerts in Prometheus

Integrate alerts into Prometheus using a `PrometheusRule` resource. Adjust alert thresholds to meet your specific operational needs.

Further information on the metrics used for these alerts can be found on the [metrics](https://docs.kuadrant.io/kuadrant-operator/doc/observability/metrics/) page.
