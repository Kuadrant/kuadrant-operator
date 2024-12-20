# Install and Configure Kuadrant and Sail via OLM using the kubectl CLI

This document will walk you through setting up the required configuration to install kaudrant using [kustomize](https://kustomize.io/) or a tool that leverages kustomize such as kubectl along with OLM. It will also go through more advanced configuration options to enable building up a resilient configuration. You can view the full configuration built here: [Full AWS Example](https://github.com/Kuadrant/kuadrant-operator/tree/main/config/install/full-example-aws).



1. [Basic Install](#basic-installation)

2. [Configure DNS and TLS integration](#configure-dns-and-tls-integration)

3. [External Redis for Rate Limit Counters](#use-an-external-redis)

4. [Limitador Resilient Configuration](#limitador-topologyconstraints-poddisruptionbudget-and-resource-limits)

5. [Authorino Resilient Configuration](#authorino-topologyconstraints-poddisruptionbudget-and-resource-limits)

4. [[OpenShift Specific] Setup Observability ](#set-up-observability-openshift-only)


## Prerequisites  
- OCP or K8s cluster and CLI available.
- OLM installed [operator lifecycle manager releases](https://github.com/operator-framework/operator-lifecycle-manager/releases)
- (Optional) Gateway Provider Installed: By default this guide will install the [Sail Operator](https://github.com/istio-ecosystem/sail-operator) that will configure and install an Istio installation. Kuadrant is intended to work with [Istio](https://istio.io) or [Envoy Gateway](https://gateway.envoyproxy.io/) as a gateway provider before you can make use of Kuadrant one of these providers should be installed.  
- (Optional) cert-manager:
  - [cert-manager Operator for Red Hat OpenShift](https://docs.openshift.com/container-platform/4.16/security/cert_manager_operator/cert-manager-operator-install.html)
  - [installing cert-manager via OperatorHub](https://cert-manager.io/docs/installation/operator-lifecycle-manager/)
- (Optional) Access to AWS, Azure or GCP with DNS services. 
- (Optional) Accessible Redis instance, for persistent storage for your rate limit counters.



> Note: for multiple clusters, it would make sense to do the installation via a tool like [argocd](https://argo-cd.readthedocs.io/en/stable/). For other methods of addressing multiple clusters take a look at the [kubectl docs](https://kubernetes.io/docs/tasks/access-application-cluster/configure-access-multiple-clusters/)

## Basic Installation

This first step will install just Kuadrant at a given released version (post v1.x) in the `kuadrant-system` namespace and the Sail Operator. There will be no credentials/dns providers configured (This is the most basic setup but means TLSPolicy and DNSPolicy will not be able to be used). 

Start by creating the following `kustomization.yaml` in a directory locally. For the purpose of this doc, we will use: `~/kuadrant/` directory.

```bash
export KUADRANT_DIR=~/kuadrant
mkdir -p $KUADRANT_DIR/install
touch $KUADRANT_DIR/install/kustomization.yaml

```

> Setting the version to install: You can set the version of kuadrant to install by adding / changing the `?ref=v1.0.1` in the resource links.

```yaml
# add this to the kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - https://github.com/Kuadrant/kuadrant-operator//config/install/standard?ref=v1.0.1 #set the versio by adding ?ref=v1.0.1 change this version as needed (see https://github.com/Kuadrant/kuadrant-operator/releases)
  #- https://github.com/Kuadrant/kuadrant-operator//config/install/openshift?ref=v1.0.1 #use if targeting an OCP cluster. Change this version as needed (see https://github.com/Kuadrant/kuadrant-operator/releases).

patches: # remove this subscription patch if you are installing a development version. It will then use the "preview" channel
  - patch: |-
      apiVersion: operators.coreos.com/v1alpha1
      kind: Subscription
      metadata:
        name: kuadrant
      spec:
        source: kuadrant-operator-catalog
        sourceNamespace: kuadrant-system
        name: kuadrant-operator
        channel: 'stable' #set to preview if not using a release (for example if using main)

```

And execute the following to apply it to a cluster:

```bash
# change the location depending on where you created the kustomization.yaml
kubectl apply -k $KUADRANT_DIR/install

```

#### Verify the operators are installed:

OLM should begin installing the dependencies for Kuadrant. To wait for them to be ready, run:

```bash
kubectl -n kuadrant-system wait --timeout=160s --for=condition=Available deployments --all
```

> Note: you may see ` no matching resources found ` if the deployments are not yet present.

Once OLM has finished installing the operators (this can take several minutes). You should see the following in the kuadrant-system namespace:

```bash
kubectl get deployments -n kuadrant-system

## Output (kuadrant-console-plugin deployment only installed on OpenShift)
# NAME                                    READY   UP-TO-DATE   AVAILABLE   AGE
# authorino-operator                      1/1     1            1           83m
# dns-operator-controller-manager         1/1     1            1           83m
# kuadrant-console-plugin                 1/1     1            1           83m
# kuadrant-operator-controller-manager    1/1     1            1           83m
# limitador-operator-controller-manager   1/1     1            1           83m

```

You can also view the subscription for information about the install:

```bash
kubectl get subscription -n kuadrant-system -o=yaml

```

### Install the operand components

Kuadrant has 2 additional operand components that it manages: `Authorino` that provides data plane auth integration and `Limitador` that provides data plane rate limiting. To set these up lets add a new `kustomization.yaml` in a new sub directory. We will re-use this later for further configuration. We do this as a separate step as we want to have the operators installed first.

Add the following to your local directory.  For the purpose of this doc, we will use: `$KUADRANT_DIR/configure/kustomization.yaml`.

```bash
mkdir -p $KUADRANT_DIR/configure
touch $KUADRANT_DIR/configure/kustomization.yaml

```

Add the following to the new kustomization.yaml:


```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - https://github.com/Kuadrant/kuadrant-operator//config/install/configure/standard?ref=v1.0.1 #change this version as needed (see https://github.com/Kuadrant/kuadrant-operator/releases)

```

Lets apply this to your cluster:

```bash

kubectl apply -k $KUADRANT_DIR/configure

```

### Verify kuadrant is installed and ready:

```bash
kubectl get kuadrant kuadrant -n kuadrant-system -o=wide

# NAME       STATUS   AGE
# kuadrant   Ready    109s

```

You should see the condition with type `Ready`. 


### Verify Istio is configured and ready:

```bash
kubectl get istio -n gateway-system

#sample output
# NAME      REVISIONS   READY   IN USE   ACTIVE REVISION   VERSION   AGE
# default   1           1       1        Healthy           v1.23.0   3d22h
```



At this point Kuadrant is installed and ready to be used as is Istio as the gateway provider. This means AuthPolicy and RateLimitPolicy can now be configured and used to protect any Gateways you create. 


## (Optional) Configure DNS and TLS integration

In this section will build on the previous steps and expand the `kustomization.yaml` we created in `$KUADRANT_DIR/configure`. 

In order for cert-manager and the Kuadrant DNS operator to be able to access and manage DNS records and setup TLS certificates and provide external connectivity for your endpoints, you need to setup a credential for these components. To do this, we will use a Kubernetes secret via a kustomize secret generator. You can find other example overlays for each supported cloud provider under the  [configure directory](https://github.com/Kuadrant/kuadrant-operator/tree/main/config/install/configure).

An example lets-encrypt certificate issuer is provided, but for more information on certificate issuers take a look at the [cert-manager documentation](https://cert-manager.io/docs/configuration/acme/).


Lets modify our existing local kustomize overlay to setup these secrets and the cluster certificate issuer:

First you will need to setup the required `.env` file specified in the kuztomization.yaml file in the same directory as your existing configure kustomization. Below is an example for AWS:

```bash
touch $KUADRANT_DIR/configure/aws-credentials.env

```
Add the following to your new file

```
AWS_ACCESS_KEY_ID=xxx
AWS_SECRET_ACCESS_KEY=xxx
AWS_REGION=eu-west-1

```

With this setup, lets update our configure kustomization to generate the needed secrets. We will also define a TLS ClusterIssuer (see below). The full `kustomization.yaml` file should look like:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - https://github.com/Kuadrant/kuadrant-operator//config/install/configure/standard?ref=v1.0.1 #change this version as needed (see https://github.com/Kuadrant/kuadrant-operator/releases)
  - cluster-issuer.yaml #(comment if you dont want to use it. The issuer yaml is defined below). Ensure you name the file correctly.
  

generatorOptions:
  disableNameSuffixHash: true
  labels:
    app.kubernetes.io/part-of: kuadrant
    app.kubernetes.io/managed-by: kustomize

secretGenerator:
  - name: aws-provider-credentials
    namespace: cert-manager # assumes cert-manager namespace exists.
    envs:
      - aws-credentials.env # notice this matches the .env file above. You will need to setup this file locally
    type: 'kuadrant.io/aws'
  - name: aws-provider-credentials
    namespace: gateway-system # this is the namespace where your gateway will be provisioned
    envs:
      - aws-credentials.env #notice this matches the .env file above. you need to set up this file locally first. 
    type: 'kuadrant.io/aws'


```

Below is an example Lets-Encrypt Cluster Issuer that uses the aws credential we setup above. Create this in the same directory as the configure kustomization.yaml:

```bash
touch $KUADRANT_DIR/configure/cluster-issuer.yaml
```

Add the following to this new file:

```yaml
# example lets-encrypt cluster issuer that will work with the credentials we will add
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: lets-encrypt-aws
spec:
  acme:
    privateKeySecretRef:
      name: le-secret
    server: https://acme-v02.api.letsencrypt.org/directory
    solvers:
      - dns01:
          route53:
            accessKeyIDSecretRef:
              key: AWS_ACCESS_KEY_ID
              name: aws-provider-credentials #notice this matches the name of the secret we created.
            region: us-east-1 #override if needed
            secretAccessKeySecretRef:
              key: AWS_SECRET_ACCESS_KEY
              name: aws-provider-credentials

```

To apply our changes (note this doesn't need to be done in different steps, but is done so here to illustrate how you can build up your configuration of Kuadrant) execute:

```bash
kubectl apply -k $KUADRANT_DIR/configure
```

The cluster issuer should become ready:

```bash
kubectl get clusterissuer -o=wide

# NAME               READY   STATUS                                                 AGE
# lets-encrypt-aws   True    The ACME account was registered with the ACME server   14s

```

We create two credentials. One for use with `DNSPolicy` in the gateway-system namespace and one for use by cert-manager in the `cert-manager` namespace. With these credentials in place and the cluster issuer configured. You are now ready to start using DNSPolicy and TLSPolicy to secure and connect your Gateways.


## Use an External Redis

To connect `Limitador` (the component responsible for rate limiting requests) to redis so that its counters are stored and can be shared with other limitador instances follow these steps:

Again we will build on the kustomization we started. In the same way we did for the cloud provider credentials, we need to setup a `redis-credential.env` file in the same directory as the kustomization.


```bash
touch $KUADRANT_DIR/configure/redis-credentials.env

```

Add the redis connection string to this file in the following format:

```
URL=redis://xxxx
```

Next we need to add a new secret generator to our existing configure file at `$KUADRANT_DIR/configure/kustomization.yaml` add it below the other `secretGenerators`

```yaml
  - name: redis-credentials
    namespace: kuadrant-system
    envs:
      - redis-credentials.env
    type: 'kuadrant.io/redis'
```

We also need to patch the existing `Limitador` resource. Add the following to the `$KUADRANT_DIR/configure/kustomization.yaml`


```yaml

patches:
  - patch: |-
      apiVersion: limitador.kuadrant.io/v1alpha1
      kind: Limitador
      metadata:
        name: limitador
        namespace: kuadrant-system
      spec:
        storage:
          redis:
            configSecretRef:
              name: redis-credentials

```

Your full `kustomize.yaml` will now be:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - https://github.com/Kuadrant/kuadrant-operator//config/install/configure/standard?ref=v1.0.1 #change this version as needed (see https://github.com/Kuadrant/kuadrant-operator/releases)
  - cluster-issuer.yaml #(comment if you dont want to use it. The issuer yaml is defined below). Ensure you name the file correctly.


generatorOptions:
  disableNameSuffixHash: true
  labels:
    app.kubernetes.io/part-of: kuadrant
    app.kubernetes.io/managed-by: kustomize

secretGenerator:
  - name: aws-provider-credentials
    namespace: cert-manager # assumes cert-manager namespace exists.
    envs:
      - aws-credentials.env # notice this matches the .env file above. You will need to setup this file locally
    type: 'kuadrant.io/aws'
  - name: aws-provider-credentials
    namespace: gateway-system # this is the namespace where your gateway will be provisioned
    envs:
      - aws-credentials.env #notice this matches the .env file above. you need to set up this file locally first.
    type: 'kuadrant.io/aws'
  - name: redis-credentials
    namespace: kuadrant-system
    envs:
      - redis-credentials.env
    type: 'kuadrant.io/redis'

patches:
  - patch: |-
      apiVersion: limitador.kuadrant.io/v1alpha1
      kind: Limitador
      metadata:
        name: limitador
        namespace: kuadrant-system
      spec:
        storage:
          redis:
            configSecretRef:
              name: redis-credentials

```


Re-Apply the configuration to setup the new secret and configuration:

```bash
kubectl apply -k $KUADRANT_DIR/configure/
```

Limitador is now configured to use the provided redis connection URL as a data store for rate limit counters. Limitador will become temporarily unavailable as it restarts.

### Validate

Validate Kuadrant is in a ready state as before:

```bash
kubectl get kuadrant kuadrant -n kuadrant-system -o=wide

# NAME       STATUS   AGE
# kuadrant   Ready    61m

```


## Resilient Deployment of data plane components

### Limitador: TopologyConstraints, PodDisruptionBudget and Resource Limits

To set limits, replicas and a `PodDisruptionBudget` for limitador you can add the following to the existing limitador patch in your local `limitador` in the `$KUADRANT_DIR/configure/kustomize.yaml` spec:

```yaml
pdb:
  maxUnavailable: 1
replicas: 2
resourceRequirements:
    requests:
      cpu: 10m
      memory: 10Mi # set these based on your own needs.
```

re-apply the configuration. This will result in two instances of limitador becoming available and a `podDisruptionBudget` being setup:

```bash
kubectl apply -k $KUADRANT_DIR/configure/

```

For topology constraints, you will need to patch the limitador deployment directly:

add the below `yaml` to a `limitador-topoloy-patch.yaml` file under a `$KUADRANT_DIR/configure/patches` directory:

```bash
mkdir -p $KUADRANT_DIR/configure/patches
touch $KUADRANT_DIR/configure/patches/limitador-topoloy-patch.yaml
```

```yaml
spec:
  template:
    spec:
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels:
              limitador-resource: limitador
        - maxSkew: 1
          topologyKey: kubernetes.io/zone
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels:
              limitador-resource: limitador

```

Apply this to the existing limitador deployment

```bash
kubectl patch deployment limitador-limitador -n kuadrant-system --patch-file $KUADRANT_DIR/configure/patches/limitador-topoloy-patch.yaml
```

### Authorino: TopologyConstraints, PodDisruptionBudget and Resource Limits

To increase the number of replicas for Authorino add a new patch to the `$KUADRANT_DIR/configure/kustomization.yaml`

```yaml
  - patch: |-
      apiVersion: operator.authorino.kuadrant.io/v1beta1
      kind: Authorino
      metadata:
        name: authorino
        namespace: kuadrant-system
      spec:
        replicas: 2

```

and re-apply the configuration:

```bash
kubectl apply -k $KUADRANT_DIR/configure/
```

To add resource limits and or topology constraints to Authorino. You will need to patch the Authorino deployment directly:
Add the below `yaml` to a `authorino-topoloy-patch.yaml` under the `$KUADRANT_DIR/configure/patches` directory:

```bash
touch $KUADRANT_DIR/configure/patches/authorino-topoloy-patch.yaml
```

```yaml
spec:
  template:
    spec:
      containers:
        - name: authorino
          resources:
            requests:
              cpu: 10m # set your own needed limits here
              memory: 10Mi # set your own needed limits here
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels:
              authorino-resource: authorino
        - maxSkew: 1
          topologyKey: kubernetes.io/zone
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels:
              authorino-resource: authorino

```

Apply the patch:

```bash
kubectl patch deployment authorino -n kuadrant-system --patch-file $KUADRANT_DIR/configure/patches/authorino-topoloy-patch.yaml
```

Kuadrant is now installed and ready to use and the data plane components are configured to be distributed and resilient.

For reference the full configure kustomization should look like:
```yaml
kind: Kustomization
resources:
  - https://github.com/Kuadrant/kuadrant-operator//config/install/configure/standard?ref=v1.0.1 #change this version as needed (see https://github.com/Kuadrant/kuadrant-operator/releases)
  - cluster-issuer.yaml
generatorOptions:
  disableNameSuffixHash: true
  labels:
    app.kubernetes.io/part-of: kuadrant
    app.kubernetes.io/managed-by: kustomize

secretGenerator:
  - name: aws-provider-credentials
    namespace: cert-manager # assumes cert-manager namespace exists.
    envs:
      - aws-credentials.env # notice this matches the .env file above. You will need to setup this file locally
    type: 'kuadrant.io/aws'
  - name: aws-provider-credentials
    namespace: gateway-system # this is the namespace where your gateway will be provisioned
    envs:
      - aws-credentials.env #notice this matches the .env file above. you need to set up this file locally first.
    type: 'kuadrant.io/aws'
  - name: redis-credentials
    namespace: kuadrant-system
    envs:
      - redis-credentials.env
    type: 'kuadrant.io/redis'

patches:
  - patch: |-
      apiVersion: limitador.kuadrant.io/v1alpha1
      kind: Limitador
      metadata:
        name: limitador
        namespace: kuadrant-system
      spec:
        pdb:
          maxUnavailable: 1
        replicas: 2
        resourceRequirements:
          requests:
            cpu: 10m
            memory: 10Mi # set these based on your own needs.
        storage:
          redis:
            configSecretRef:
              name: redis-credentials
  - patch: |-
      apiVersion: operator.authorino.kuadrant.io/v1beta1
      kind: Authorino
      metadata:
        name: authorino
        namespace: kuadrant-system
      spec:
        replicas: 2

```
The configure directory should contain the following:

```
configure/
├── aws-credentials.env
├── cluster-issuer.yaml
├── kustomization.yaml
├── patches
│   ├── authorino-topoloy-patch.yaml
│   └── limitador-topoloy-patch.yaml
└── redis-credentials.env
```

## Set up observability (OpenShift Only)

Verify that user workload monitoring is enabled in your Openshift cluster.
If it not enabled, check the [Openshift documentation](https://docs.openshift.com/container-platform/4.17/observability/monitoring/enabling-monitoring-for-user-defined-projects.html) for how to do this.


```bash
kubectl get configmap cluster-monitoring-config -n openshift-monitoring -o jsonpath='{.data.config\.yaml}'|grep enableUserWorkload
# (expected output)
# enableUserWorkload: true
```

Install the gateway & Kuadrant metrics components and configuration, including Grafana.

```bash
# change the version as needed
kubectl apply -k https://github.com/Kuadrant/kuadrant-operator//config/install/configure/observability?ref=v1.0.1
```

Configure the Openshift thanos-query instance as a data source in Grafana.

```bash
TOKEN="Bearer $(oc whoami -t)"
HOST="$(kubectl -n openshift-monitoring get route thanos-querier -o jsonpath='https://{.status.ingress[].host}')"
echo "TOKEN=$TOKEN" > config/observability/openshift/grafana/datasource.env
echo "HOST=$HOST" >> config/observability/openshift/grafana/datasource.env
kubectl apply -k config/observability/openshift/grafana
```

Create the example dashboards in Grafana

```bash
kubectl apply -k https://github.com/Kuadrant/kuadrant-operator//examples/dashboards?ref=v1.0.1
```

Access the Grafana UI, using the default user/pass of root/secret.
You should see the example dashboards in the 'monitoring' folder.
For more information on the example dashboards, check out the [documentation](https://docs.kuadrant.io/latest/kuadrant-operator/doc/observability/examples/).

```bash
kubectl -n monitoring get routes grafana-route -o jsonpath="https://{.status.ingress[].host}"
```


### Next Steps

- Try out one of our user-guides [secure, connect protect](https://docs.kuadrant.io/latest/kuadrant-operator/doc/user-guides/full-walkthrough/secure-protect-connect-k8s/#overview)
