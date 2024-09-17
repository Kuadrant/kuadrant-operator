## Orphan DNS Records

This document is focused around multi-cluster DNS where you have more than one instance of a gateway that shares a common hostname with other gateways and assumes you have the [observability](https://docs.kuadrant.io/0.10.0/kuadrant-operator/doc/observability/examples/) stack set up.

### What is an orphan record?

An orphan DNS record is a record or set of records that are owned by an instance of the DNS operator that no longer has a representation of those records on its cluster.

### How do orphan records occur?

Orphan records can occur when a `DNSRecord` resource (a resource that is created in response to a `DNSPolicy`) is deleted without allowing the owning controller time to clean up the associated records in the DNS provider. Generally in order for this to happen, you would need to force remove a `finalizer` from the `DNSRecord` resource, delete the kuadrant-system namespace directly or un-install kuadrant (delete the subscription if using OLM) without first cleaning up existing policies or delete a cluster entirely without first cleaning up the associated DNSPolicies. These are not common scenarios but when they do occur they can leave behind records in your DNS Provider which may point to IPs / Hosts that are no longer valid. 


### How do you spot an orphan record(s) exist?

There is an a prometheus based based alert that we have created that uses some metrics exposed from the DNS components to spot this situation. If you have installed the alerts for Kuadrant under the examples folder, you will see in the alerts tab an alert called `PossibleOrphanedDNSRecords`. When this is firing it means there are likely to be orphaned records in your provider.

### How do you get rid of an orphan record?

To remove an Orphan Record we must first identify the owner of that record that is no longer aware of the record. To do this we need an existing DNSRecord in another cluster.

Example: You have 2 clusters that each have a gateway and share a host `apps.example.com` and have setup a DNSPolicy for each gateway. On cluster 1 you remove the `kuadrant-system` namespace without first cleaning up existing DNSPolicies targeting the gateway in your `ingress-gateway` namespace. Now there are a set of records that were being managed for that gateway that have not been removed. 
On cluster 2 the DNS Operator managing the existing DNSRecord in that cluster has a record of all owners of that dns name. 
In prometheus alerts, it spots that the number of owners does not correlate to the number of DNSRecord resources and triggers an alert. 
To remedy this rather than going to the DNS provider directly and trying to figure out which records to remove, you can instead follow the steps below.

1) Get the owner id of the DNSRecord on cluster 2 for the shared host 

```
kubectl get dnsrecord somerecord -n my-gateway-ns -o=jsonpath='{.status.ownerID}'
```

2) get all the owner ids

```
kubectl get dnsrecord.kuadrant.io somerecord -n my-gateway-ns -o=jsonpath='{.status.domainOwners}'

## output
["26aacm1z","49qn0wp7"]
```

3) create a placeholder DNSRecord with none active ownerID


for each owner id returned that isn't the owner id of the record we got earlier that we want to remove records for, we need to create a dnsrecord resource and delete it. This will trigger the running operator in this cluster to clean up those records.

```
# this is one of the owner id **not** in the existing dnsrecord on cluster
export ownerID=26aacm1z  

export rootHost=$(kubectl get dnsrecord.kuadrant.io somerecord -n  my-gateway-ns -o=jsonpath='{.spec.rootHost}')

# export a namespace with the aws credentials in it
export targetNS=kuadrant-system 

kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: DNSRecord
metadata:
  name: delete-old-loadbalanced-dnsrecord
  namespace: ${targetNS}
spec:
  providerRef:
    name: my-aws-credentials
  ownerID: ${ownerID}
  rootHost: ${rootHost}
  endpoints:
    - dnsName: ${rootHost}
      recordTTL: 60
      recordType: CNAME
      targets:
        - klb.doesnt-exist.${rootHost}
EOF

```

4) Delete the dnsrecord

```
kubectl delete dnsrecord.kuadrant.io delete-old-loadbalanced-dnsrecord -n ${targetNS} 
```

5) verify 

We can verify by checking the dnsrecord again. Note it may take a several minutes for the other record to update. We can force it by adding a label to the record

```
kubectl label dnsrecord.kuadrant.io somerecord test=test -n ${targetNS}

kubectl get dnsrecord.kuadrant.io somerecord -n my-gateway-ns -o=jsonpath='{.status.domainOwners}'

```

We should also see our alert eventually stop triggering also.
