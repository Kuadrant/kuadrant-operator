# Kuadrant Operator

[![Code Style](https://github.com/Kuadrant/kuadrant-operator/actions/workflows/code-style.yaml/badge.svg)](https://github.com/Kuadrant/kuadrant-operator/actions/workflows/code-style.yaml)
[![Testing](https://github.com/Kuadrant/kuadrant-operator/actions/workflows/test.yaml/badge.svg)](https://github.com/Kuadrant/kuadrant-operator/actions/workflows/test.yaml)
[![codecov](https://codecov.io/gh/Kuadrant/kuadrant-operator/branch/main/graph/badge.svg?token=4Z16KPS3HT)](https://codecov.io/gh/Kuadrant/kuadrant-operator)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/9242/badge)](https://www.bestpractices.dev/projects/9242)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2FKuadrant%2Fkuadrant-operator.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2FKuadrant%2Fkuadrant-operator?ref=badge_shield)

## Overview

Kuadrant leverages [Gateway API](https://gateway-api.sigs.k8s.io/) and [Policy Attachment](https://gateway-api.sigs.k8s.io/geps/gep-2648/) to enhance gateway providers like [Istio](https://istio.io/latest/docs/tasks/traffic-management/ingress/gateway-api/) and [Envoy Gateway](https://gateway.envoyproxy.io/) with additional features via Policies. Those features include TLS, DNS, application authentication & authorization, and rate limiting.

You can find more information on the different aspects of Kuadrant at the documentation links below:

- [Overview](https://docs.kuadrant.io)
- [Getting Started & Installation](https://docs.kuadrant.io/dev/getting-started/)
- [Architecture](https://docs.kuadrant.io/dev/architecture/docs/design/architectural-overview-v1/)

## Contributing

The [Development guide](doc/overviews/development.md) describes how to build the kuadrant operator and
how to test your changes before submitting a patch or opening a PR.

Join us on the [#kuadrant](https://kubernetes.slack.com/archives/C05J0D0V525) channel in the Kubernetes Slack workspace,
for live discussions about the roadmap and more.

## Licensing

This software is licensed under the [Apache 2.0 license](https://www.apache.org/licenses/LICENSE-2.0).

See the LICENSE and NOTICE files that should have been provided along with this software for details.

[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2FKuadrant%2Fkuadrant-operator.svg?type=large)](https://app.fossa.com/projects/git%2Bgithub.com%2FKuadrant%2Fkuadrant-operator?ref=badge_large)
