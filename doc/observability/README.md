# Observability stack guide

## Deploying the observabilty

Make sure to enable the observability reconciler in your Kuadrant CR, this will create service monitors for the kuadrant, dns, limitador and authorino operators:
```yaml
kind: Kuadrant
apiVersion: kuadrant.io/v1beta1
metadata:
  name: kuadrant
  namespace: kuadrant-system
spec:
    observability:
        enable: true
```


```bash
./bin/kustomize build ./config/observability/| docker run --rm -i docker.io/ryane/kfilt -i kind=CustomResourceDefinition | kubectl apply --server-side -f -
./bin/kustomize build ./config/observability/| docker run --rm -i docker.io/ryane/kfilt -x kind=CustomResourceDefinition | kubectl apply -f -
./bin/kustomize build ./config/thanos | kubectl apply -f -
./bin/kustomize build ./examples/dashboards | kubectl apply -f -
./bin/kustomize build ./examples/alerts | kubectl apply -f -
THANOS_RECEIVE_ROUTER_IP=$(kubectl -n monitoring get svc thanos-receive-router-lb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
kubectl -n monitoring patch prometheus k8s --type='merge' -p '{"spec":{"remoteWrite":[{"url":"http://'"$THANOS_RECEIVE_ROUTER_IP"':19291/api/v1/receive", "writeRelabelConfigs":[{"action":"replace", "replacement":"'"$KUADRANT_CLUSTER_NAME"'", "targetLabel":"cluster_id"}]}]}}'
```

This will deploy prometheus, alertmanager and grafana into the `monitoring` namespace,
along with metrics scrape configuration for Istio and Envoy.
Thanos will also be deployed with prometheus configured to remote write to it.

If you are using Istio as your gateway provider, run this command to configure & scrape Istio specific metrics

```bash
./bin/kustomize build ./config/observability/prometheus/monitors/istio | kubectl apply -f -
```

If you are using Envoy Gateway as your gateway provider, run this command to configure & scrape Envoy Gateway specific metrics:

```bash
./bin/kustomize build ./config/observability/prometheus/monitors/envoy | kubectl apply -f -
```

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

Following the steps in [Editing dashboards](#editing-dashboards), export the dashboard json into `examples/dashboards/` with the toggle "Export for sharing manually" on. Once all dashboard json files sare saved/updated, run the following make target to sanitise it for use in both the Grafana Community Platform for sharing, and for use as a mounted configmap volume locally.

```bash
make dashboard-cleanup
```

Now, you have a universal dashboard file you can use to import into your Grafana instance, but also use for uploading to the Grafana Community Platform.
