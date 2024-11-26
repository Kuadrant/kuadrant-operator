# Load Balanced DNS

## Overview

This document will show you how to setup a load balanced DNS configuration using the (DNSPolicy)[https://docs.kuadrant.io/latest/kuadrant-operator/doc/reference/dnspolicy/] API. When we say "load balanced", this means we configure the DNS provider (AWS, GCP etc) to return different gateway/loadbalancer addresses to queries from DNS clients based on specific weighting and geo location configuration.


### When should I use a load balanced DNS policy?

It is most useful to use the load balancing options when targeting multiple gateways that share a listener host E.G (api.example.com). It is also perfectly valid to use it when you only have a single gateway; this provides the benefit of allowing you to easily expand beyond this single gateway for a given shared hostname. It is worth knowing that the load balanced DNSpolicy comes with a relatively small additional cost of some added records and lookups during DNS resolution vs a "simple" DNSPolicy with no load balancing specified as the latter only sets up a simple A or CNAME record. So in summary if you expect to need multiple gateways for a given listener host then you should take advantage of the load balanced option.


### Important Considerations

- When using a DNSPolicy with a load balanced configuration, all DNSPolicies effecting a listener with the same hostname should have load balanced options set. Without the load balanced configuration, Kuadrant's dns controller will try to set up only a simple A or CNAME record.
- When setting geographic configuration, only ever set one unique GEO as the default GEO across all instances of DNSPolicy targeting a listener with the same hostname. If you set different defaults for a single listener hostname, the dns controllers will constantly attempt to bring the default into the state they each feel is correct. 
- If you want different load balancing options for a particular listener in a gateway, you can target that listener directly with DNSPolicy via the targetRef sectionName property.
- If you do not use the load balanced configuration, a simple single A or CNAME record is set up. Later if you need to move to load balanced, you will need to delete and recreate your policy.

## DNS Provider Setup

A DNSPolicy acts against a target Gateway or a target listener within a gateway by processing the hostnames on the targeted listeners. Using these it can create dns records using the address exposed in the Gateway's status block. In order for Kuadrant's DNS component to do this, it must be able to access and know which DNS provider to use. This is done through the creation of a dns provider secret containing the needed credentials and the provider identifier.

(Learn more about how to setup a DNS Provider)[https://docs.kuadrant.io/latest/dns-operator/docs/provider/]


## LoadBalanced DNSPolicy creation and attachment

Once an appropriate provider credential is configured, we can now create and attach a DNSPolicy to start managing DNS for the listeners on our Gateway. Below is an example.

```yaml
apiVersion: kuadrant.io/v1
kind: DNSPolicy
metadata:
  name: prod-web
  namespace: ingress-gateway
spec:
  targetRef:
    name: prod-web
    group: gateway.networking.k8s.io
    kind: Gateway
    sectionName: listenerName 
  providerRef:
    name: my-aws-credentials 
  loadBalancing:
    weight: 120 
    geo: GEO-EU 
    defaultGeo: true

```


### Load Balancing section

This section must be filled out and indicates to the dns component that the targets of this policy should be setup to handle more than one gateway. It is required to define values for the weighted and geo options. These values are used for the records created by the policy controller based on the target gateway.
To read more detail about each of the fields in the loadbalanced section take a look at [DNS Overview](https://docs.kuadrant.io/latest/kuadrant-operator/doc/dns/#high-level-example-and-field-definition)



##### Locations supported per DNS provider

| Supported     | AWS | GCP |
|---------------|-----|-----|
| Continents    | :white_check_mark: |  :x: |
| Country codes | :white_check_mark: |  :x:  |
| States        | :white_check_mark: |  :x:  |
| Regions       |  :x:  | :white_check_mark: |  

##### Continents and country codes supported by AWS Route 53

:**Note:** :exclamation: For more information please the official AWS documentation 

To see all regions supported by AWS Route 53, please see the official (documentation)[https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/resource-record-sets-values-geo.html]. With Route 53 when setting a continent code use a "GEO-" prefix otherwise it will be considered a country code. 

##### Regions supported by GCP Cloud DNS

To see all regions supported by GCP Cloud DNS, please see the official (documentation)[https://cloud.google.com/compute/docs/regions-zones]

##### Regions and Countries supported by Azure Cloud DNS

To see the different values you can use for the geo based DNS with Azure take a look at the following (documentation)[https://learn.microsoft.com/en-us/azure/traffic-manager/traffic-manager-geographic-regions]

