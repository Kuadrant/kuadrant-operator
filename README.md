# Kuadrant Operator

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0)

## Overview

The Operator to install and manage the lifecycle of the [Kuadrant](https://github.com/Kuadrant/) components deployments.

### Provided APIs

The kuadrant control plane owns the following [Custom Resource Definitions, CRDs](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/):

| CRD                                                                                                                                                 | Description |
|-----------------------------------------------------------------------------------------------------------------------------------------------------| --- |
| RateLimitPolicy CRD [\[doc\]](https://github.com/Kuadrant/kuadrant-controller/blob/main/doc/rate-limiting.md) [[reference]](https://github.com/Kuadrant/kuadrant-controller/blob/main/doc/ratelimitpolicy-reference.md) | Enable access control on workloads based on HTTP rate limiting |
| [AuthPolicy CRD](https://github.com/Kuadrant/kuadrant-controller/blob/main/apis/apim/v1alpha1/authpolicy_types.go)                                                                                            | Enable AuthN and AuthZ based access control on workloads |

Additionally, kuadrant provides the following CRDs

| CRD | Owner | Description |
| --- | --- | --- |
| [Kuadrant CRD](https://github.com/Kuadrant/kuadrant-operator/blob/main/api/v1beta1/kuadrant_types.go) | [Kuadrant Operator](https://github.com/Kuadrant/kuadrant-operator) | Represents an instance of kuadrant |
| [Limitador CRD](doc/ratelimitpolicy-reference.md) | [Limitador Operator](https://github.com/Kuadrant/limitador-operator) | Represents an instance of Limitador |
| [Authorino CRD](https://github.com/Kuadrant/authorino-operator#the-authorino-custom-resource-definition-crd) | [Authorino Operator](https://github.com/Kuadrant/authorino-operator) | Represents an instance of Authorino |
| [AuthConfig CRD](https://github.com/Kuadrant/authorino/blob/main/docs/architecture.md#the-authorino-authconfig-custom-resource-definition-crd) | [Authorino](https://github.com/Kuadrant/authorino) | The desired authN and authZ protection for a service |
Examples:

#### Kuadrant CR
```yaml
---
apiVersion: kuadrant.kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant-sample
spec: {}
```

#### RateLimitPolicy CR
```yaml
---
apiVersion: apim.kuadrant.io/v1alpha1
kind: RateLimitPolicy
metadata:
  name: toystore
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  rateLimits:
    - rules:
        - paths: ["/toy"]
          methods: ["GET"]
      configurations:
        - actions:
            - generic_key:
                descriptor_key: get-operation
                descriptor_value: "1"
      limits:
        - conditions:
            - "get-operation == 1"
          maxValue: 2
          seconds: 5
          variables: []
```

#### AuthPolicy CR
```yaml
---
apiVersion: apim.kuadrant.io/v1alpha1
kind: AuthPolicy
metadata:
  name: toystore
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  rules:
    - hosts: ["*.toystore.com"]
      paths: ["/toy*"]
  authScheme:
    hosts: ["api.toystore.com"]
    identity:
      - name: friends
        apiKey:
          labelSelectors:
            app: toystore
        credentials:
          in: authorization_header
          keySelector: APIKEY
    response:
      - json:
          properties:
            - name: user-id
              value: null
              valueFrom:
                authJSON: auth.identity.metadata.annotations.secret\.kuadrant\.io/user-id
        name: rate-limit-apikey
        wrapper: envoyDynamicMetadata
        wrapperKey: ext_auth_data
```

## Licensing

This software is licensed under the [Apache 2.0 license](https://www.apache.org/licenses/LICENSE-2.0).

See the LICENSE and NOTICE files that should have been provided along with this software for details.
