# Using Gateway API and Kuadrant with APIs outside of the cluster


### Overview

In some cases, the application and API endpoints are exposed in a host external to the cluster where you are a running Gateway API and Kuadrant but you do not want it accessible directly via the public internet. If you want to have external traffic come into a Gateway API defined Gateway and protected by Kuadrant policies first being proxied to the existing legacy endpoints, this guide will give you some example of how to achieve this.


### What we will do
- Have an API in a private location become accessible via a public hostname
- Setup a gateway and HTTPRoute to expose this private API via our new Gateway on a (public) domain.
- proxy valid requests through to our back-end API service
- Add auth and rate limiting and TLS to our public Gateway to protect it



### Pre Requisites

- [Kuadrant and Gateway API installed (with Istio as the gateway provider)](https://docs.kuadrant.io/latest/kuadrant-operator/doc/install/install-kubernetes/) 
- Existing API on separate cluster accessible via HTTP from the Gateway cluster


What we want to achieve:

```
                                ------------------- DMZ -----------------|
                                                                         |
                               |-------------------------------- internal network -----------------------------------| 
                    load balancer                                        |                                            |           
                        | - |  |      |----------k8s cluster-----------| |   |----- Legacy API Location --------|     |
                        |   |  |      |  Gateway  Kuadrant             | |   |                                  |     |       
                        |   |  |      |   -----    -----               | |   |                                  |     |                     
---public traffic--my.api.com-------->|   |    |<--|   |               | |   |  HTTP (my.api.local)   Backend   |     |
                        |   |  |      |   |    |   -----               | |   |      -----             -----     |     | 
                        |   |  |      |   ----- -----------proxy---(my.api.local)-->|   | ----------> |   |     |     | 
                        |   |  |      |                                | |   |      -----             -----     |     | 
                        | - |  |      |--------------------------------| |   |----------------------------------|     | 
                               |                                         |                                            |   
                               |-----------------------------------------|--------------------------------------------| 
                                                                         |
                                ------------------- DMZ -----------------|       
```


Note for all of the resources defined here there is a copy of them under the [examples folder](https://github.com/Kuadrant/kuadrant-operator/examples/external-api-istio.yaml)

1) Deploy a Gateway into the K8s cluster that will act as the main Ingress Gateway

Define your external API hostname and Internal API hostname

```
export EXTERNAL_HOST=my.api.com
export INTERNAL_HOST=my.api.local

```

```bash
kubectl apply -n gateway-system -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  labels:
    istio: ingress
  name: ingress
spec:
  gatewayClassName: istio
  listeners:
    - name: ingress-tls
      port: 443
      hostname: '${EXTERNAL_HOST}'
      protocol: HTTPS
      allowedRoutes:
        namespaces:
          from: All
      tls:
        mode: Terminate
        certificateRefs:
          - name: ingress-tls  #you can use TLSPolicy to provide this certificate or provide it manually
            kind: Secret
EOF            
```

2) Optional: Use TLSPolicy to configure TLS certificates for your listeners

[TLSPolicy Guide](https://docs.kuadrant.io/latest/kuadrant-operator/doc/user-guides/gateway-tls/)

3) Optional: Use DNSPolicy to bring external traffic to the external hostname

[DNSPolicy Guide](https://docs.kuadrant.io/latest/kuadrant-operator/doc/user-guides/gateway-dns/#create-a-dns-provider-secret)

4) Ensure the Gateway has the status of `Programmed` set to `True` meaning it is ready. 

```bash
kubectl get gateway ingress -n gateway-system -o=jsonpath='{.status.conditions[?(@.type=="Programmed")].status}'
```

5) Let Istio know about the external hostname and the rules it should use when sending traffic to that destination.

Create a [`ServiceEntry`](https://istio.io/latest/docs/reference/config/networking/service-entry/)

```bash
kubectl apply -n gateway-system -f - <<EOF
apiVersion: networking.istio.io/v1beta1
kind: ServiceEntry
metadata:
  name: internal-api
spec:
  hosts:
    - ${INTERNAL_HOST} # your internal http endpoint
  location: MESH_EXTERNAL
  resolution: DNS
  ports:
    - number: 80
      name: http
      protocol: HTTP
    - number: 443
      name: https
      protocol: TLS
EOF
```


Create a [`DestionationRule`](https://istio.io/latest/docs/reference/config/networking/destination-rule/) to configure how to handle traffic to this endpoint.

```bash
kubectl apply -n gateway-system -f - <<EOF
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: internal-api
spec:
  host: ${INTERNAL_HOST}
  trafficPolicy:
    tls:
      mode: SIMPLE
      sni: ${INTERNAL_HOST}
EOF
```


6) Create a `HTTPRoute` that will route traffic for the Gateway and re-write the host

```bash
kubectl apply -n gateway-system -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: external-host
spec:
  parentRefs:
    - name: ingress
  hostnames:
    - ${EXTERNAL_HOST}
  rules:
    - backendRefs:
        - name: ${INTERNAL_HOST}
          kind: Hostname
          group: networking.istio.io
          port: 443
      filters:
        - type: URLRewrite
          urlRewrite:
            hostname: ${INTERNAL_HOST}
EOF
```

We should now be able to send requests to our external host and have the Gateway proxy requests and responses to and from the internal host.

7) (optional) Add Auth and RateLimiting to protect your public endpoint

As we are using Gateway API to define the Gateway and HTTPRoutes, we can now also apply RateLimiting and Auth to protect our public endpoints

[AuthPolicy Guide](https://docs.kuadrant.io/latest/kuadrant-operator/doc/user-guides/auth-for-app-devs-and-platform-engineers/)

[RateLimiting Guide](https://docs.kuadrant.io/latest/kuadrant-operator/doc/user-guides/gateway-rl-for-cluster-operators/)

