# Understanding DNS Policy Delegation

Delegation in context of DNS policy is to pass the reconciliation responsibility to a **primary cluster**.
This is a multi cluster feature, with two clusters types, referred to as **primary cluster** and **secondary cluster**.

A **primary cluster** is a cluster that will reconcile delegated dns policies into an **authoritative dns record**.
An **authoritative dns record** is a dns record that the *dns-operator* manages, and consists of all the delegated dns records for a root host.
The *kuadrant-operator* translates dns policies into dns records that the *dns-operator* can understand.
As a user of the *kuadrant-operator* there is no need to interact with the dns records, they are mentioned here for understanding of the workflow.

The **primary cluster** requires a default provider secret, which is labelled with `kujadrant.io/default-provider=true`.
The *dns-operator* can have the `--delegation-role=primary` adding to the `args`, or `data.DELEGATION_ROLE: primary` to the `dns-operator-controller-env` configmap.
This is not strictly necessary as the default delegation role is primary.
Multi cluster communication is done via a secret with the label `kuadrant.io/multicluster-kubeconfig=true`.
These secrets are created within the same namespace as the *dns-operator* deployment.
The secret contains a kubeconfig that allow access to a cluster within the multi cluster setup, and there will be one secret pre **secondary cluster**.
The `kubectl_kuadrant-dns` plugin provides a command to help with the secret generation.
See the [CLI documentation](https://github.com/Kuadrant/dns-operator/blob/main/docs/cli.md), and `kubectl_kuadrant-dns add-cluster-secret --help` for more information.
If there are multiply **primary clusters**, each cluster must have the same cluster connection secrets, and a cluster connection secrets to the other **primary cluster**.
The **primary cluster B** will generate an identical **authoritative dns record** to **primary cluster A**.

A **secondary cluster** is a cluster that will **not** reconcile delegated dns policies.
The underlining *dns-operator* pass the reconciliation of the dns records to the **primary cluster**.
*Kuatrant-operator* will maintain the dns policy as normal.
As the **secondary cluster** does not interact with the dns provider, there is no need for a provider secret.
To configure a **secondary cluster** the *dns-opeator* deployment requires `--delegation-role=secondary` added to `args`.
This can be configured within the `dns-operator-controller-env` configmap with `data.DELEGATION_ROLE: secondary`.
An important note is a cluster in secondary mode can still reconcile dns policies that do not have the delegation field set to true.
When a dns polices on a **secondary cluster** is configured without delegation, the *dns-operator* works the same as a single cluster install where *kuadrant-operator* generates the dns record from the dns policy.

The delegation of a dns policy is achieved by setting `delegate=true` in the dns policy spec.
Due to limitations of multi cluster communication, the `delegate` field is immutable,
Changing this field requires the removal of the dns policy from the cluster, and recreation with the newer values.
The `delegate=true`, and `providerRef` are mutually exclusive, and can not be set together.
A delegated dns policy works in the same manner on a **primary cluster** as a **secondary**.
This allows multiply **primary clusters** to operate on the dns policies, but also allow the resigning of clusters roles at a later stage without having to recreate the dns policy on the cluster.


