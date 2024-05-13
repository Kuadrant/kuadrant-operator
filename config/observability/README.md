# Observability stack guide

## Deploying the observabilty stack

```bash
./bin/kustomize build ./config/observability/| docker run --rm -i ryane/kfilt -i kind=CustomResourceDefinition | kubectl apply --server-side -f -
./bin/kustomize build ./config/observability/| docker run --rm -i ryane/kfilt -x kind=CustomResourceDefinition | kubectl apply -f -
```

This will deploy prometheus, alertmanager and grafana into the `monitoring` namespace,
along with metrics scrape configuration for Istio and Envoy.

## Accessing Grafana & Prometheus

Use port forwarding to access Grafana & Prometheus:

```bash
kubectl -n monitoring port-forward service/grafana 3000:3000
```

The Grafana UI can be found at [http://127.0.0.1:3000/](http://127.0.0.1:3000/) (default user/pass of `admin` & `admin`).
It is pre-loaded with some kubernetes and [gateway-api-state](https://github.com/Kuadrant/gateway-api-state-metrics) dashboards.

```bash
kubectl -n monitoring port-forward service/prometheus-k8s 9090:9090
```

The Prometheus UI can be found at [http://127.0.0.1:9090](http://127.0.0.1:9090).

## Editing dashboards

Dashboards can be imported in the Grafana UI using either raw JSON, a JSON file, or the URL/ID of one of the [dashboards on grafana.com](https://grafana.com/grafana/dashboards/).
Some example dashboards are available in the [/examples](/examples) folder.

To import a dashboard, click on the plus icon on the left sidebar and navigate to **Import**. After entering a dashboard URL/ID or JSON, click **Load**.

After loading the dashboard, the next screen allows you to select a name and folder for the dashboard and specify the data source before finally importing the dashboard.

Grafana dashboards can be exported as JSON in order to add them to the project's git repo.
When viewing the dashboard you wish to export, click on the **share** button at the top of the screen.

In the modal popup click **Export** and then **Save to file**.

## Editing alerting rules

Alerting rules can be defined in [PrometheusRules](https://github.com/prometheus-operator/prometheus-operator/blob/main/Documentation/user-guides/alerting.md#configuring-alertmanager-in-prometheus) resources.
The can be viewed in the Prometheus UI Alerts tab.
Some example alerting rules are available in the [/examples](/examples) folder.

## Exporting a dashboard for use with Grafana Community Platform or other Grafana Instances

Following the steps in [Editing dashboards](#editing-dashboards), we need to make two exports - one where the toggle "Export for sharing manually" is toggled, and one where it isn't. 

- In order for Grafana Community Platform to accept the dashboard upon upload, it needs to know what is required (i.e. Grafana version, panels, Prometheus version) for the dashboard to function correctly. Without this information, an error is thrown saying the format of the dashboard JSON is too old.

- However, for the Grafana instance to accept the dashboard upon import, the option for selecting the data source is required, as the generated data source for sharing externally may not be what the data source is for a user's Grafana instance. If the generated data source was used, the user may not have that data source configured, and Grafana will throw an error to that effect.

Therefore, we will be making a "hybrid" dashboard that utilizes specifying what is required (i.e. Grafana version, panels, Prometheus version) but also giving the choice back to the user to decide which data source they would like to use. This results in a dashboard that is compatible with both Grafana instance dashboard imports, and is also compatible with a Grafana Community Platform dashboard upload.

Once both of these JSON files are exported and saved correctly, ensuring their names are differentiable, we can now combine both JSONs to form our "universal" dashboard.

1. Open both JSONs side to side for ease of editing.
2. In the "Export for sharing manually" JSON, copy the "__requires" field and paste it in an outermost fashion into the default export JSON near the top. It should look like:

```json
{
"__requires": [
    {
      "type": "panel",
      "id": "dashlist",
      "name": "Dashboard list",
      "version": ""
    },
    {
      "type": "grafana",
      "id": "grafana",
      "name": "Grafana",
      "version": "8.5.5"
    },
    {
      "type": "datasource",
      "id": "prometheus",
      "name": "Prometheus",
      "version": "1.0.0"
    },
    {
      "type": "panel",
      "id": "stat",
      "name": "Stat",
      "version": ""
    },
    {
      "type": "panel",
      "id": "table",
      "name": "Table",
      "version": ""
    },
    {
      "type": "panel",
      "id": "text",
      "name": "Text",
      "version": ""
    },
    {
      "type": "panel",
      "id": "timeseries",
      "name": "Time series",
      "version": ""
    }
  ],
  "annotations": {
        .
        .
        .
  },
  .
  .
  .
  "weekStart": ""
}
```
3. Save this file and use as needed. 

Note that we have retained the default data source rather than having an abstract one, allowing the user to make the choice of which data source they wish to use. We have also allowed for the requirements to be specified for uploading the dashboard to the Grafana Community Platform.