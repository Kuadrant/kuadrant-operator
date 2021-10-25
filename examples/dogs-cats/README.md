# Dogs and Cats API


## Install kuadrant

The install command will create a namespace called `kuadrant-system` and deploy kuadrant services in that namespace.

```bash
kuadrantctl install
```

## Deploy all

```bash
kubectl apply -f examples/dogs-cats
```


## Test

Run kubectl port-forward in a different shell:

```bash
❯ kubectl port-forward -n kuadrant-system service/kuadrant-gateway 9080:80
Forwarding from [::1]:9080 -> 8080
```

The service can now be accessed at `http://localhost:9080` via a browser or any other client, like curl.

```bash
❯ curl -H "Authorization: APIKEY JUSTFORDEMOSOBVIOUSLYqDQsqSPMHkRhriEOtcRx" -H "Host: api.animaltoys.127.0.0.1.nip.io" localhost:9080/cats/toys

❯ curl -H "Authorization: APIKEY JUSTFORDEMOSOBVIOUSLYqDQsqSPMHkRhriEOtcRx" -H "Host: api.animaltoys.127.0.0.1.nip.io" localhost:9080/dogs/toys
```
