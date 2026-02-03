# CoreDNS Integration Guide

## Table of Contents

1. [Overview](#overview)
2. [Purpose and Use Cases](#purpose-and-use-cases)
3. [Infrastructure Requirements](#infrastructure-requirements)
4. [Deploying CoreDNS](#deploying-coredns)
5. [Configuring CoreDNS](#configuring-coredns)
6. [Testing the Deployment](#testing-the-deployment)
7. [Advanced Configuration: Configuring an Edge Server](#advanced-configuration-configuring-an-edge-server)
8. [Appendix](#appendix)

---

## Overview

CoreDNS integration enables DNS Operator to manage DNS records using a self-hosted CoreDNS instance running in Kubernetes, providing an alternative to cloud-managed DNS services (AWS Route53, Google Cloud DNS, Azure DNS).

> [!NOTE]
> **Reversibility**: CoreDNS integration can be removed by deleting the CoreDNS deployment and switching DNSPolicies to use a different DNS provider. To migrate to a different provider, delete existing DNSRecords and recreate them with the new provider secret reference. No data is permanently locked into CoreDNS.

### Is This for You?

Consider CoreDNS integration if you:
- Need to avoid dependency on external cloud DNS services
- Operate in environments with no internet access or restricted network connectivity
- Have regulatory or compliance requirements mandating self-hosted infrastructure
- Want to delegate specific DNS zones from existing DNS servers (BIND, Unbound, or cloud DNS) to Kubernetes-managed CoreDNS
- Require consistent DNS management across hybrid or multi-cloud environments
- Need to reduce DNS operational costs by eliminating per-query charges

Cloud DNS providers might be a simpler choice if you are running exclusively in cloud environments and none of these scenarios apply.

**Audience**: Platform engineers, DevOps engineers, and system administrators deploying DNS infrastructure.

---

## Purpose and Use Cases

Using CoreDNS integration, you can self-manage your DNS infrastructure in Kubernetes environments. Organizations choose CoreDNS integration to maintain full control over their DNS records, meet regulatory compliance requirements, avoid cloud provider dependencies, or integrate with existing DNS infrastructure through zone delegation.

### How It Works

Unlike cloud DNS providers where a DNS Operator "pushes" records through API calls, CoreDNS integration works through a Kubernetes-native **label-based watch mechanism**:

1. **DNS Operator processes DNSRecords**
2. **DNS Operator adds a label** `kuadrant.io/coredns-zone-name: <zone>` to processed DNSRecords
3. **CoreDNS plugin watches** for DNSRecords with this label via Kubernetes API
4. **CoreDNS serves the records** it discovers through the watch

**The label is what activates CoreDNS integration**, not a provider secret or API call. This design allows CoreDNS to remain a passive observer of Kubernetes resources while DNS Operator handles the orchestration logic.

### Primary Use Cases

#### Self-Hosted Infrastructure

Organizations that require full control over their DNS infrastructure without relying on external cloud services.

**Scenarios**:
- **On-Premise Deployments**: Running Kubernetes clusters in your own data centers or colocation facilities
- **Cost Optimization**: Eliminating per-query or per-zone costs from cloud DNS services
- **Complete Ownership**: Maintaining full control over DNS data and infrastructure

**Key Benefits**:
- Complete ownership and control of DNS records
- No external service dependencies
- Reduced latency for local queries
- Integration with existing data center infrastructure
- Lower operational costs for DNS services

#### Restricted and Isolated Environments

Deployments where external connectivity is limited, prohibited, or subject to strict compliance requirements.

**Scenarios**:
- **Air-Gapped Networks**: Completely disconnected environments with no external internet access
- **Regulatory Compliance**: Government, defense, financial, or healthcare sectors with mandatory network isolation
- **Data Sovereignty**: Requirements to keep DNS data within specific geographic or organizational boundaries
- **Security-Critical Systems**: Highly sensitive environments requiring complete audit trails and access control
- **Development/Test Isolation**: Isolated environments that mirror production security constraints

**Key Benefits**:
- Fully self-contained DNS infrastructure
- Meets security and compliance mandates
- No external network dependencies
- Complete audit trails within controlled boundaries
- Reduced attack surface by eliminating external services

#### Zone Delegation from Edge DNS Servers

Integration with existing DNS infrastructure by delegating specific zones to CoreDNS while maintaining authoritative control at the edge.

**Scenario**:
- **Edge DNS Integration**: Delegate application-specific subdomains (e.g., `apps.example.com`) from your authoritative DNS servers (BIND, Unbound, or cloud DNS providers like Route53, Cloud DNS, Azure DNS) to CoreDNS

**Key Benefits**:
- Seamless integration with existing DNS infrastructure (self-hosted or cloud-based)
- Preserve existing DNS operations and processes
- Maintain edge DNS security controls and governance
- Flexible deployment without replacing entire DNS infrastructure

---

## Infrastructure Requirements

### CoreDNS Infrastructure

For CoreDNS infrastructure requirements (resource limits, storage, networking, etc.), see the [CoreDNS deployment documentation](https://coredns.io/manual/toc/#installation).

### DNS Operator Integration Requirements

**Minimum Kubernetes Version**: 1.19.0 or higher

**Cluster Architecture**:
- **Single Cluster**: One Kubernetes cluster running CoreDNS and DNS Operator
- **Multi-Cluster**: Multiple clusters with different roles:
  - **Primary Clusters**: Run CoreDNS instance and DNS Operator in primary mode
  - **Secondary Clusters**: Run DNS Operator only in secondary mode (no CoreDNS deployment needed)

**RBAC Permissions**:

CoreDNS pods require Kubernetes API permissions to watch DNSRecord resources:
- **DNSRecord resources** (`kuadrant.io` API group): `get`, `list`, `watch` permissions
- **Scope**: Configured with the `WATCH_NAMESPACES` environment variable (empty = all namespaces, which is the default)

### Multi-Cluster Requirements

For multi-cluster delegation scenarios:

**Network Connectivity**:

Primary clusters must have network connectivity to secondary cluster Kubernetes APIs. Required for synchronizing DNSRecord status across clusters

**Cluster Interconnection Secrets**:

Primary clusters require kubeconfig interconnection secrets for all other clusters (primary and secondary):
- The secret must be created using the `kubectl-kuadrant_dns add-cluster-secret` command (from the `dns-operator` CLI, which must be installed separately - see [CLI documentation](https://github.com/Kuadrant/dns-operator/blob/main/docs/cli.md))
- Labeled with `kuadrant.io/multicluster-kubeconfig=true`
- Stored in `dns-operator-system` namespace

**DNS Operator Configuration**:
- Primary clusters: `--delegation-role=primary` (default)
- Secondary clusters: `--delegation-role=secondary`

For complete details, see [DNS Record Delegation](../user-guides/dns/understanding_dns_delegation.md)

---

## Deploying CoreDNS

> [!IMPORTANT]
> When DNS Operator is using CoreDNS, it can do so in both single-cluster and multi-cluster settings. However, **for multi-cluster environments, the Delegation feature is required**. Cloud DNS providers can operate in multi-cluster mode with or without delegation, but CoreDNS specifically requires delegation for multi-cluster coordination. See [DNS Record Delegation](../user-guides/dns/understanding_dns_delegation.md) for complete details.

### Deployment Architectures

**Single Cluster Deployment**:

A standalone deployment where all CoreDNS components run in a single Kubernetes cluster. Suitable for development, testing, or standalone deployments. No delegation required.

**What Gets Deployed:**
- CoreDNS instance (with Kuadrant plugin) - serves DNS queries
- DNS Operator - reconciles DNSRecords

**Multi-Cluster Deployment**:

Multi-cluster deployments use the delegation feature to coordinate DNS records across clusters. For complete details on how delegation works, see [DNS Record Delegation](../user-guides/dns/understanding_dns_delegation.md).

**What Gets Deployed:**

- **Primary Clusters**:
  - CoreDNS instance - serves DNS queries
  - DNS Operator in primary mode (`--delegation-role=primary`)

- **Secondary Clusters**:
  - DNS Operator only in secondary mode (`--delegation-role=secondary`)
  - No CoreDNS deployment needed

### Deployment Guides

Follow the step-by-step deployment guides for your scenario:

**Local Environment**:
- [Single Cluster Setup](https://github.com/Kuadrant/dns-operator/blob/main/docs/coredns/README.md#setup-single-cluster-local-environment-kind) - Deploy to a local cluster for development and testing
- [Multi-Cluster Setup](https://github.com/Kuadrant/dns-operator/blob/main/docs/coredns/README.md#setup-multi-cluster-local-environment-kind) - Deploy to multiple local clusters with delegation

**Existing Clusters**:
- [Deployment Guide](https://github.com/Kuadrant/kuadrant-operator/blob/main/doc/user-guides/dns/core-dns.md) - Deploy to existing Kubernetes clusters

### Next Steps

After deploying CoreDNS:
- For configuration details (Corefile, provider secrets, monitoring, logging, etc.), see [Configuring CoreDNS](#configuring-coredns)
- For verification procedures, see [Testing the Deployment](#testing-the-deployment)

---

## Configuring CoreDNS

CoreDNS configuration consists of two main components:

1. **Corefile**: Defines zones, plugins, and DNS server behavior
2. **Provider Secrets**: Enable DNS Operator to coordinate zone matching with CoreDNS

### Core Configuration

**Corefile**:
- Zone definitions matching the zones CoreDNS serves with plugin directives and configuration
- Essential plugin: `kuadrant`

**Provider Secrets**:
- **Non-delegating DNSRecords** require a CoreDNS provider secret reference (with the `providerRef` or default provider label) for DNS Operator to add the zone label
- **Delegating DNSRecords** do not require provider secrets themselves, but they cause authoritative DNSRecords to be created on primary clusters, which **do require provider secrets**

**Zone Coordination**:
Zones must be listed in both the Corefile and provider secret `ZONES` fields to ensure that the zones CoreDNS and DNS Operator manage are specified.

### Optional Configuration

- **Monitoring**: Prometheus metrics, Grafana dashboards
- **Logging**: Debug, error, and query logging

### Configuration Reference

For complete technical details, see **[CoreDNS Configuration Reference](https://github.com/Kuadrant/dns-operator/blob/main/docs/coredns/configuration.md)**

### Additional Resources

- **[DNS Record Delegation](../user-guides/dns/understanding_dns_delegation.md)** - Multi-cluster delegation feature and provider secret requirements
- **[Kuadrant CoreDNS Plugin Documentation](https://github.com/Kuadrant/dns-operator/blob/main/coredns/plugin/README.md)** - Plugin-specific configuration
- **[CoreDNS Official Documentation](https://coredns.io/manual/toc/)** - General CoreDNS plugins

---

## Testing the Deployment

After deploying and configuring CoreDNS, verify that the integration is functioning correctly by testing DNS resolution, zone transfers, and advanced routing features.

### Verification Procedures

The deployment guides include comprehensive verification procedures for testing CoreDNS functionality:

**Local Environment**:
- [Single Cluster Verification](https://github.com/Kuadrant/dns-operator/blob/main/docs/coredns/README.md#verify) - Verify single cluster deployment
- [Multi-Cluster Verification](https://github.com/Kuadrant/dns-operator/blob/main/docs/coredns/README.md#verify-1) - Verify multi-cluster deployment
- [GEO Routing Verification](https://github.com/Kuadrant/dns-operator/blob/main/docs/coredns/README.md#geo) - Test geographic routing

**Existing Clusters**:
- [Verification Guide](https://github.com/Kuadrant/kuadrant-operator/blob/main/doc/user-guides/dns/core-dns.md) - Verify cluster deployments

### Troubleshooting

**Records not appearing in DNS queries**:
1. Verify DNSRecord has the `kuadrant.io/coredns-zone-name` label - DNS Operator adds this when processing the record
2. Check the zone in the label matches a zone defined in your Corefile
3. Review CoreDNS logs for watch errors or plugin issues
4. Confirm that CoreDNS has RBAC permissions to watch DNSRecords in the target namespace

**Geo-routing not working**:
1. Ensure `geoip` and `metadata` plugins are enabled in your Corefile
2. Verify GeoIP database path in Corefile matches the mounted database location
3. Enable debug logging in Corefile and review logs for GeoIP lookup failures

**Multi-cluster coordination issues**:
1. For delegation issues, see [DNS Record Delegation](../user-guides/dns/understanding_dns_delegation.md) troubleshooting
2. Verify kubeconfig secrets exist and are correctly labeled on primary clusters
3. Check DNS Operator logs on both primary and secondary clusters for reconciliation errors
4. Confirm network connectivity between clusters (primary clusters must reach secondary cluster APIs)

---

## Advanced Configuration: Configuring an Edge Server

This section covers **zone delegation** - an advanced configuration for integrating CoreDNS with existing DNS infrastructure. Zone delegation allows you to maintain your existing authoritative DNS servers while delegating specific subdomains to CoreDNS running in Kubernetes.

> [!NOTE]
> Zone delegation is **optional**. If you're running CoreDNS as your primary DNS infrastructure without integrating with existing DNS servers, you can skip this section.

### What is Zone Delegation?

Zone delegation is a DNS mechanism where an authoritative DNS server delegates responsibility for a subdomain to another set of nameservers. This creates a hierarchical DNS structure where:

1. **Parent Zone** (e.g., `example.com`) is managed by an authoritative edge server (Bind9, cloud DNS, etc.)
2. **Delegated Zone** (e.g., `k.example.com`) is delegated to CoreDNS instances running in Kubernetes
3. **Delegation Records** (NS and glue A records) in the parent zone point to the CoreDNS nameservers

**Why Use Zone Delegation?**

- **Production DNS Hierarchy**: Maintain your existing authoritative DNS while delegating application zones to Kubernetes
- **Organizational Control**: Keep root domains managed by existing DNS infrastructure and processes
- **Isolation**: Separate Kubernetes-managed DNS from corporate DNS infrastructure
- **Flexibility**: Change Kubernetes DNS infrastructure without affecting root domain configuration

**Common Scenarios**:
- Delegate `apps.example.com` from Bind9-managed `example.com` to CoreDNS
- Delegate internal zones from cloud DNS (Route53, Cloud DNS) to on-premise CoreDNS
- Create test/staging zones delegated to development CoreDNS instances

### Implementation Guide

For step-by-step instructions on configuring zone delegation with Bind9, see:
- **[Zone Delegation Guide](https://github.com/Kuadrant/dns-operator/blob/main/docs/coredns/zone-delegation.md)** - Complete guide for setting up Bind9 delegation

**What the guide covers**:
- Configuring Bind9 (deployed with `make local-setup` command) to delegate zones to CoreDNS
- Creating NS and glue A records for delegation using `nsupdate`
- Configuring cluster CoreDNS to forward queries to the edge server
- Setting up rewrite rules in `kuadrant-coredns` for active groups queries
- Verification procedures

**Production Adaptations**:
- If using an existing Bind9 server (not in Kubernetes), adapt the `nsupdate` commands to target your server
- For cloud DNS providers (Route53, Cloud DNS, Azure DNS), create NS records via their web console or API
- Ensure proper network connectivity between your edge DNS and CoreDNS LoadBalancer IPs
- Consider DNSSEC if your root zone uses it
- Implement monitoring for delegation chain health

---

## Appendix

### Reference Configurations

**Corefile Examples**:
- [Production Corefile](https://github.com/Kuadrant/dns-operator/blob/main/config/coredns/Corefile) - Example Corefile with all plugins configured

**DNSRecord Examples**:
- [Plugin README Examples](https://github.com/Kuadrant/dns-operator/blob/main/coredns/plugin/README.md) - DNSRecord examples with geo-routing and weighted routing

**Deployment Configurations**:
- [Kustomization](https://github.com/Kuadrant/dns-operator/blob/main/config/coredns/kustomization.yaml) - Kustomize configuration including Helm chart values for CoreDNS deployment

### Glossary

**Authoritative DNSRecord**: A DNSRecord created and managed by a primary cluster that merges delegating DNSRecords from all clusters in a multi-cluster setup.

**CoreDNS**: An open-source, cloud-native DNS server that serves as a flexible, extensible platform for DNS services in Kubernetes.

**Corefile**: The configuration file for CoreDNS that defines zones, plugins, and server behavior.

**Delegating DNSRecord**: A DNSRecord with `delegate: true` that is reconciled by primary clusters and merged into authoritative DNSRecords in multi-cluster deployments.

**DNS Operator**: A Kubernetes operator that manages DNS records across multiple providers (cloud and self-hosted) using DNSRecord custom resources.

**DNSRecord**: A custom resource (CRD) in the DNS Operator that represents DNS record configuration to be managed by the operator.

**GeoIP Routing**: DNS routing strategy that returns different responses based on the geographic location of the client making the query.

**Kuadrant Plugin**: The CoreDNS plugin that watches DNSRecord resources via the Kubernetes API and serves them as DNS responses.

**Label-based Watch**: The mechanism by which CoreDNS discovers DNSRecords - DNS Operator adds the `kuadrant.io/coredns-zone-name` label, and CoreDNS watches for resources with this label.

**Non-delegating DNSRecord**: A DNSRecord without `delegate: true` that requires a provider secret reference for zone matching and reconciliation.

**Primary Cluster**: A cluster running CoreDNS and DNS Operator in primary mode (`--delegation-role=primary`) that reconciles delegating DNSRecords into authoritative DNSRecords.

**Provider Secret**: A Kubernetes Secret containing provider-specific configuration (zones, credentials) that enables DNS Operator to interact with DNS providers.

**Secondary Cluster**: A cluster running only DNS Operator in secondary mode (`--delegation-role=secondary`) that creates delegating DNSRecords for primary clusters to reconcile.

**Weighted Routing**: DNS routing strategy that distributes traffic across multiple endpoints based on assigned weights.

**Zone Coordination**: The requirement that zones must be listed in both the Corefile (for CoreDNS) and provider secret `ZONES` field (for DNS Operator).

**Zone Delegation**: DNS mechanism where an authoritative DNS server delegates responsibility for a subdomain to another set of nameservers.

### Additional Resources

**CoreDNS Documentation**:
- [CoreDNS Official Documentation](https://coredns.io/manual/toc/) - General CoreDNS configuration and plugins
- [Kuadrant CoreDNS Plugin](https://github.com/Kuadrant/dns-operator/blob/main/coredns/plugin/README.md) - Plugin-specific documentation and examples

**DNS Operator Documentation**:
- [DNS Record Delegation](../user-guides/dns/understanding_dns_delegation.md) - Multi-cluster delegation feature documentation
- [Provider Documentation](https://github.com/Kuadrant/dns-operator/blob/main/docs/provider.md) - DNS provider configuration details
- [DNS Operator README](https://github.com/Kuadrant/dns-operator/blob/main/README.md) - Main project documentation

**Deployment Guides**:
- [Local Single Cluster Setup](https://github.com/Kuadrant/dns-operator/blob/main/docs/coredns/README.md#setup-single-cluster-local-environment-kind) - Deploy to a local cluster
- [Local Multi-Cluster Setup](https://github.com/Kuadrant/dns-operator/blob/main/docs/coredns/README.md#setup-multi-cluster-local-environment-kind) - Deploy to multiple local clusters with delegation
- [Cluster Deployment](https://github.com/Kuadrant/kuadrant-operator/blob/main/doc/user-guides/dns/core-dns.md) - Deploy to existing Kubernetes clusters
- [Edge Server Configuration](https://github.com/Kuadrant/dns-operator/blob/main/docs/coredns/configure-edge-server.md) - Configure Bind9 zone delegation

**Configuration References**:
- [CoreDNS Configuration Reference](https://github.com/Kuadrant/dns-operator/blob/main/docs/coredns/configuration.md) - Detailed technical configuration guide

**External Resources**:
- [Kuadrant Architecture - CoreDNS Integration](https://github.com/Kuadrant/architecture/blob/main/docs/design/core-dns-integration.md)
- [Understanding DNS Delegation](https://github.com/Kuadrant/kuadrant-operator/blob/main/doc/user-guides/dns/understanding_dns_delegation.md)
- [CoreDNS Use Cases Blog Post](https://kuadrant.io/blog/core-dns-support/)
