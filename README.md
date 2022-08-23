# Kuadrant Operator

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0)

## Overview

The Operator to install and manage the lifecycle of the [Kuadrant](https://github.com/Kuadrant/) components deployments.

### Provided APIs

The kuadrant control plane owns the following [Custom Resource Definitions, CRDs](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/):

| CRD                                                                                                                                                                                                                     | Description                                                    | Example                                                                                                                               |
|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------|
| RateLimitPolicy CRD [\[doc\]](https://github.com/Kuadrant/kuadrant-controller/blob/main/doc/rate-limiting.md) [[reference]](https://github.com/Kuadrant/kuadrant-controller/blob/main/doc/ratelimitpolicy-reference.md) | Enable access control on workloads based on HTTP rate limiting | [RateLimitPolicy CR](https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/main/config/samples/kuadrant_v1beta1_kuadrant.yaml) |
| [AuthPolicy CRD](https://github.com/Kuadrant/kuadrant-controller/blob/main/apis/apim/v1alpha1/authpolicy_types.go)                                                                                                      | Enable AuthN and AuthZ based access control on workloads       | [AuthPolicy CR](https://github.com/Kuadrant/kuadrant-controller/blob/main/config/samples/apim_v1alpha1_ratelimitpolicy.yaml)          |

Additionally, Kuadrant provides the following CRDs

| CRD                                                                                                          | Owner                                                                | Description                         | Example                                                                                                                           |
|--------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------------|-------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------|
| [Kuadrant CRD](https://github.com/Kuadrant/kuadrant-operator/blob/main/api/v1beta1/kuadrant_types.go)        | [Kuadrant Operator](https://github.com/Kuadrant/kuadrant-operator)   | Represents an instance of kuadrant  | [Kuadrant CR](https://github.com/Kuadrant/kuadrant-operator/blob/main/config/samples/kuadrant_v1beta1_kuadrant.yaml)              |
| [Limitador CRD](doc/ratelimitpolicy-reference.md)                                                            | [Limitador Operator](https://github.com/Kuadrant/limitador-operator) | Represents an instance of Limitador | [Limitador CR](https://github.com/Kuadrant/limitador-operator/blob/main/config/samples/limitador_v1alpha1_limitador.yaml)         |
| [Authorino CRD](https://github.com/Kuadrant/authorino-operator#the-authorino-custom-resource-definition-crd) | [Authorino Operator](https://github.com/Kuadrant/authorino-operator) | Represents an instance of Authorino | [Authorino CR](https://github.com/Kuadrant/authorino-operator/blob/main/config/samples/authorino-operator_v1beta1_authorino.yaml) |

## Licensing

This software is licensed under the [Apache 2.0 license](https://www.apache.org/licenses/LICENSE-2.0).

See the LICENSE and NOTICE files that should have been provided along with this software for details.
