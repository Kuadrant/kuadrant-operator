# Install Kuadrant on an OpenShift Cluster

Note these steps need to be taken on each cluster you want to use Kuadrant on.

## Pre Requisites

- OpenShift 4.14.x with community operator catalog available
- AWS account with route 53 and zone 
- Accessible Redis Instance

## Setup Environment

```
export AWS_ACCESS_KEY_ID=xxxxxxx # the key id from AWS
export AWS_SECRET_ACCESS_KEY=xxxxxxx # the access key from AWS
export REDIS_URL=redis://user:xxxxxx@some-redis.com:10340

```

## Installing the dependencies

Kuadrant integrates with Istio as a Gateway API provider. Before trying Kuadrant, we need to setup an Istio based Gateway API provider. For this we will use the `Sail` operator. 

- Install v1 of Gateway API

```
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.0.0/standard-install.yaml
```

- ### Install and Configure Istio via the Sail Operator


#### Install
```
kubectl create ns istio-system
```

```
kubectl  apply -f - <<EOF
kind: OperatorGroup
apiVersion: operators.coreos.com/v1
metadata:
  annotations:
    argocd.argoproj.io/sync-wave: "0"
  name: sail
  namespace: istio-system
spec: 
  upgradeStrategy: Default  
---  
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: sailoperator
  namespace: istio-system
spec:
  channel: 3.0-dp1
  installPlanApproval: Automatic
  name: sailoperator
  source: community-operators
  sourceNamespace: openshift-marketplace
EOF
```

To check the status of the install you can run:

```
kubectl get installplan -n istio-system -o=jsonpath='{.items[0].status.phase}'
```

once ready it will be marked `complete` while installing it will be marked `installing`

#### Configure Istio

```
kubectl apply -f - <<EOF
apiVersion: operator.istio.io/v1alpha1
kind: Istio
metadata:
  annotations:
    argocd.argoproj.io/sync-wave: "1"
  name: default
spec:
  version: v1.21.0
  namespace: istio-system
  # Disable autoscaling to reduce dev resources
  values:
    pilot:
      autoscaleEnabled: false
EOF
```

Wait for Istio to be ready

```
kubectl wait istio/default -n istio-system --for="condition=Ready=true"
```


## TODO Thanos and Configure Observability


### Install Kuadrant

To install Kuadrant we will use the Kuadrant Operator. Before this lets set up some secrets we will need later.

```

kubectl create ns kuadrant-system

```

AWS Credential for TLS verification

```
kubectl -n kuadrant-system create secret generic aws-credentials \
  --type=kuadrant.io/aws \
  --from-literal=AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID \
  --from-literal=AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY
````  

Redis Credential for shared counters

```
kubectl -n kuadrant-system create secret generic redis-config \
  --from-literal=URL=$REDIS_URL  
```  

```
kubectl create ns ingress-gateway
```

AWS Credential for managing DNS records

```
kubectl -n ingress-gateway create secret generic aws-credentials \
  --type=kuadrant.io/aws \
  --from-literal=AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID \
  --from-literal=AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY
```  

To install the Kuadrant operator:

**Optional**:
Setup the catalog. Note this step is only needed if you want to use the latest cutting edge.
```



kubectl apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: kuadrant-operator-catalog
  namespace: kuadrant-system
spec:
  sourceType: grpc
  image: quay.io/kuadrant/kuadrant-operator-catalog:latest
  displayName: Kuadrant Operators
  publisher: grpc
  updateStrategy:
    registryPoll:
      interval: 45m
EOF
```      
Install the Kuadrant Operator
```
kubectl apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  annotations:
    argocd.argoproj.io/sync-wave: "0"
  name: kuadrant-operator
  namespace: kuadrant-system
spec:
  channel: preview
  installPlanApproval: Automatic
  name: kuadrant-operator
  source: kuadrant-operator-catalog
  sourceNamespace: kuadrant-system
---
kind: OperatorGroup
apiVersion: operators.coreos.com/v1
metadata:
  name: kuadrant
  namespace: kuadrant-system
spec: 
  upgradeStrategy: Default 
EOF
```  

Wait for kuadrant operators to be installed

```
kubectl get installplan -n kuadrant-system -o=jsonpath='{.items[0].status.phase}'
```

This should return `complete`

#### Configure Kuadrant

```
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  annotations:
    argocd.argoproj.io/sync-wave: "1"
  name: kuadrant
  namespace: kuadrant-system
spec:
  limitador:
    storage:
      redis:
        configSecretRef:
          name: redis-config 
EOF          
```      

Wait for Kuadrant to be ready

```
kubectl wait kuadrant/kuadrant --for="condition=Ready=true" -n kuadrant-system --timeout=300s
```

Kuadrant is now ready to use. 

#TODO add link to follow on guide