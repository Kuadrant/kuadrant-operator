# The AuthPolicy Custom Resource Definition (CRD)

- [AuthPolicy](#authpolicy)
- [AuthPolicySpec](#authpolicyspec)
  - [AuthScheme](#authscheme)
    - [AuthRuleCommon](#authrulecommon)
    - [AuthenticationRule](#authenticationrule)
    - [MetadataRule](#metadatarule)
    - [AuthorizationRule](#authorizationrule)
    - [ResponseSpec](#responsespec)
      - [SuccessResponseSpec](#successresponsespec)
        - [SuccessResponseItem](#successresponseitem)
    - [CallbackRule](#callbackrule)
  - [NamedPattern](#namedpattern)
  - [AuthPolicyCommonSpec](#authPolicyCommonSpec)
- [AuthPolicyStatus](#authpolicystatus)
  - [ConditionSpec](#conditionspec)

## AuthPolicy

| **Field** | **Type**                              | **Required** | **Description**                                 |
|-----------|---------------------------------------|:------------:|-------------------------------------------------|
| `spec`    | [AuthPolicySpec](#authpolicyspec)     | Yes          | The specification for AuthPolicy custom resource |
| `status`  | [AuthPolicyStatus](#authpolicystatus) | No           | The status for the custom resource              |

## AuthPolicySpec

| **Field**        | **Type**                                                                                                                                    | **Required** | **Description**                                                                                                                                                                                                                                                                                 |
|------------------|---------------------------------------------------------------------------------------------------------------------------------------------|--------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `targetRef`      | [LocalPolicyTargetReference](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1alpha2.LocalPolicyTargetReference) | Yes          | Reference to a Kubernetes resource that the policy attaches to                                                                                                                                                                                                                                  |
| `rules`          | [AuthScheme](#authscheme)                                                                                                                   | No           | Implicit default authentication/authorization rules                                                                                                                                                                                                                                             |
| `patterns`       | Map<String: [NamedPattern](#namedpattern)>                                                                                                  | No           | Implicit default named patterns of lists of `selector`, `operator` and `value` tuples, to be reused in `when` conditions and pattern-matching authorization rules.                                                                                                                              |
| `when`           | [][PatternExpressionOrRef](https://docs.kuadrant.io/latest/authorino/docs/features/#common-feature-conditions-when)                                | No           | List of implicit default additional dynamic conditions (expressions) to activate the policy. Use it for filtering attributes that cannot be expressed in the targeted HTTPRoute's `spec.hostnames` and `spec.rules.matches` fields, or when targeting a Gateway.                                |
| `defaults`       | [AuthPolicyCommonSpec](#authPolicyCommonSpec)                                                                                               | No           | Explicit default definitions. This field is mutually exclusive with any of the implicit default definitions: `spec.rules`, `spec.routeSelectors`, `spec.patterns`, `spec.when`                                                                                                                  |
| `overrides`      | [AuthPolicyCommonSpec](#authPolicyCommonSpec)                                                                                               | No           | Atomic overrides definitions. This field is mutually exclusive with any of the implicit or explicit default definitions: `spec.rules`, `spec.routeSelectors`, `spec.patterns`, `spec.when`, `spec.default`                                                                                      |


## AuthPolicyCommonSpec

| **Field**        | **Type**                                                                                                                                    | **Required** | **Description**                                                                                                                                                                                                                                                                |
|------------------|---------------------------------------------------------------------------------------------------------------------------------------------|--------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `rules`          | [AuthScheme](#authscheme)                                                                                                                   | No           | Authentication/authorization rules                                                                                                                                                                                                                                             |
| `patterns`       | Map<String: [NamedPattern](#namedpattern)>                                                                                                  | No           | Named patterns of lists of `selector`, `operator` and `value` tuples, to be reused in `when` conditions and pattern-matching authorization rules.                                                                                                                              |
| `when`           | [][PatternExpressionOrRef](https://docs.kuadrant.io/latest/authorino/docs/features/#common-feature-conditions-when)                                | No           | List of additional dynamic conditions (expressions) to activate the policy. Use it for filtering attributes that cannot be expressed in the targeted HTTPRoute's `spec.hostnames` and `spec.rules.matches` fields, or when targeting a Gateway.                                |

### AuthScheme

| **Field**        | **Type**                                               | **Required** | **Description**                                                                                                                                                             |
|------------------|--------------------------------------------------------|:------------:|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `authentication` | Map<String: [AuthenticationRule](#authenticationrule)> | No           | Authentication rules. At least one config MUST evaluate to a valid identity object for the auth request to be successful. If omitted or empty, anonymous access is assumed. |
| `metadata`       | Map<String: [MetadataRule](#metadatarule)>             | No           | Rules for fetching auth metadata from external sources.                                                                                                                     |
| `authorization`  | Map<String: [AuthorizationRule](#authorizationrule)>   | No           | Authorization rules. All policies MUST allow access for the auth request be successful.                                                                                     |
| `response`       | [ResponseSpec](#responsespec)                          | No           | Customizations to the response to the authorization request. Use it to set custom values for unauthenticated, unauthorized, and/or success access request.                  |
| `callbacks`      | Map<String: [CallbackRule](#callbackrule)>             | No           | Rules for post-authorization callback requests to external services. Triggered regardless of the result of the authorization request.                                       |

#### AuthRuleCommon

| **Field**               | **Type**                                                                                                     | **Required** | **Description**                                                                                                                                                                                                                                                                             |
|-------------------------|--------------------------------------------------------------------------------------------------------------|:------------:|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `when`                  | [][PatternExpressionOrRef](https://docs.kuadrant.io/latest/authorino/docs/features/#common-feature-conditions-when) | No           | List of additional dynamic conditions (expressions) to activate the auth rule. Use it for filtering attributes that cannot be expressed in the targeted HTTPRoute's `spec.hostnames` and `spec.rules.matches` fields, or when targeting a Gateway.                                          |
| `cache`                 | [Caching spec](https://docs.kuadrant.io/latest/authorino/docs/features/#common-feature-caching-cache)               | No           | Caching options for the resolved object returned when applying this auth rule. (Default: disabled)                                                                                                                                                                                          |
| `priority`              | Integer                                                                                                      | No           | Priority group of the auth rule. All rules in the same priority group are evaluated concurrently; consecutive priority groups are evaluated sequentially. (Default: `0`)                                                                                                                    |
| `metrics`               | Boolean                                                                                                      | No           | Whether the auth rule emits individual observability metrics. (Default: `false`)                                                                                                                                                                                                            |

#### AuthenticationRule

| **Field**               | **Type**                                                                                                                                                 | **Required** | **Description**                                                                                                                                                                                                                                                                                         |
|-------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------|:------------:|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `apiKey`                | [API Key authentication spec](https://docs.kuadrant.io/latest/authorino/docs/features/#api-key-authenticationapikey)                                            | No           | Authentication based on API keys stored in Kubernetes secrets. Use one of: `apiKey`, `jwt`, `oauth2Introspection`, `kubernetesTokenReview`, `x509`, `plain`, `anonymous`.                                                                                                                               |
| `kubernetesTokenReview` | [KubernetesTokenReview spec](https://docs.kuadrant.io/latest/authorino/docs/features/#kubernetes-tokenreview-authenticationkubernetestokenreview)               | No           | Authentication by Kubernetes token review. Use one of: `apiKey`, `jwt`, `oauth2Introspection`, `kubernetesTokenReview`, `x509`, `plain`, `anonymous`.                                                                                                                                                   |
| `jwt`                   | [JWT verification spec](https://docs.kuadrant.io/latest/authorino/docs/features/#jwt-verification-authenticationjwt)                                            | No           | Authentication based on JSON Web Tokens (JWT). Use one of: `apiKey`, `jwt`, `oauth2Introspection`, `kubernetesTokenReview`, `x509`, `plain`, `anonymous`.                                                                                                                                               |
| `oauth2Introspection`   | [OAuth2 Token Introscpection spec](https://docs.kuadrant.io/latest/authorino/docs/features/#oauth-20-introspection-authenticationoauth2introspection)           | No           | Authentication by OAuth2 token introspection. Use one of: `apiKey`, `jwt`, `oauth2Introspection`, `kubernetesTokenReview`, `x509`, `plain`, `anonymous`.                                                                                                                                                |
| `x509`                  | [X.509 authentication spec](https://docs.kuadrant.io/latest/authorino/docs/features/#x509-client-certificate-authentication-authenticationx509)                 | No           | Authentication based on client X.509 certificates. The certificates presented by the clients must be signed by a trusted CA whose certificates are stored in Kubernetes secrets. Use one of: `apiKey`, `jwt`, `oauth2Introspection`, `kubernetesTokenReview`, `x509`, `plain`, `anonymous`.             |
| `plain`                 | [Plain identity object spec](https://docs.kuadrant.io/latest/authorino/docs/features/#plain-authenticationplain)                                                | No           | Identity object extracted from the context. Use this method when authentication is performed beforehand by a proxy and the resulting object passed to Authorino as JSON in the auth request. Use one of: `apiKey`, `jwt`, `oauth2Introspection`, `kubernetesTokenReview`, `x509`, `plain`, `anonymous`. |
| `anonymous`             | [Anonymous access](https://docs.kuadrant.io/latest/authorino/docs/features/#anonymous-access-authenticationanonymous)                                           | No           | Anonymous access. Use one of: `apiKey`, `jwt`, `oauth2Introspection`, `kubernetesTokenReview`, `x509`, `plain`, `anonymous`.                                                                                                                                                                            |
| `credentials`           | [Auth credentials spec](https://docs.kuadrant.io/latest/authorino/docs/features/#extra-auth-credentials-authenticationcredentials)                              | No           | Customizations to where credentials are required to be passed in the request for authentication based on this auth rule. Defaults to HTTP Authorization header with prefix "Bearer".                                                                                                                    |
| `overrides`             | [Identity extension spec](https://docs.kuadrant.io/latest/authorino/docs/features/#extra-identity-extension-authenticationdefaults-and-authenticationoverrides) | No           | JSON overrides to set to the resolved identity object. Do not use it with identity objects of other JSON types (array, string, etc).                                                                                                                                                                    |
| `defaults`              | [Identity extension spec](https://docs.kuadrant.io/latest/authorino/docs/features/#extra-identity-extension-authenticationdefaults-and-authenticationoverrides) | No           | JSON defaults to set to the resolved identity object. Do not use it with identity objects of other JSON types (array, string, etc).                                                                                                                                                                     |
| _(inline)_              | [AuthRuleCommon](#authrulecommon)                                                                                                                        | No           |                                                                                                                                                                                                                                                                                                         |

#### MetadataRule

| **Field**   | **Type**                                                                                                                          | **Required** | **Description**                                                                                                                         |
|-------------|-----------------------------------------------------------------------------------------------------------------------------------|:------------:|-----------------------------------------------------------------------------------------------------------------------------------------|
| `http`      | [HTTP GET/GET-by-POST external metadata spec](https://docs.kuadrant.io/latest/authorino/docs/features/#http-getget-by-post-metadatahttp) | No           | External source of auth metadata via HTTP request. Use one of: `http`, `userInfo`, `uma`.                                               |
| `userInfo`  | [OIDC UserInfo spec](https://docs.kuadrant.io/latest/authorino/docs/features/#oidc-userinfo-metadatauserinfo)                            | No           | OpendID Connect UserInfo linked to an OIDC authentication rule declared in this same AuthPolicy. Use one of: `http`, `userInfo`, `uma`. |
| `uma`       | [UMA metadata spec](https://docs.kuadrant.io/latest/authorino/docs/features/#user-managed-access-uma-resource-registry-metadatauma)      | No           | User-Managed Access (UMA) source of resource data.  Use one of: `http`, `userInfo`, `uma`.                                              |
| _(inline)_  | [AuthRuleCommon](#authrulecommon)                                                                                                 | No           |                                                                                                                                         |

#### AuthorizationRule

| **Field**                       | **Type**                                                                                                                                                           | **Required** | **Description**                                                                                                                                        |
|---------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------|:------------:|--------------------------------------------------------------------------------------------------------------------------------------------------------|
| `patternMatching`               | [Pattern-matching authorization spec](https://docs.kuadrant.io/latest/authorino/docs/features/#pattern-matching-authorization-authorizationpatternmatching)               | No           | Pattern-matching authorization rules. Use one of: `patternMatching`, `opa`, `kubernetesSubjectAccessReview`, `spicedb`.                                |
| `opa`                           | [OPA authorization spec](https://docs.kuadrant.io/latest/authorino/docs/features/#open-policy-agent-opa-rego-policies-authorizationopa)                                   | No           | Open Policy Agent (OPA) Rego policy. Use one of: `patternMatching`, `opa`, `kubernetesSubjectAccessReview`, `spicedb`.                                 |
| `kubernetesSubjectAccessReview` | [Kubernetes SubjectAccessReview spec](https://docs.kuadrant.io/latest/authorino/docs/features/#kubernetes-subjectaccessreview-authorizationkubernetessubjectaccessreview) | No           | Authorization by Kubernetes SubjectAccessReview. Use one of: `patternMatching`, `opa`, `kubernetesSubjectAccessReview`, `spicedb`.                     |
| `spicedb`                       | [SpiceDB authorization spec](https://docs.kuadrant.io/latest/authorino/docs/features/#spicedb-authorizationspicedb)                                                       | No           | Authorization decision delegated to external Authzed/SpiceDB server. Use one of: `patternMatching`, `opa`, `kubernetesSubjectAccessReview`, `spicedb`. |
| _(inline)_                      | [AuthRuleCommon](#authrulecommon)                                                                                                                                  | No           |                                                                                                                                                        |

#### ResponseSpec

| **Field**         | **Type**                                                                                                                                                           | **Required** | **Description**                                                                                                                    |
|-------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------|:------------:|------------------------------------------------------------------------------------------------------------------------------------|
| `unauthenticated` | [Custom denial status spec](https://docs.kuadrant.io/latest/authorino/docs/features/#custom-denial-status-responseunauthenticated-and-responseunauthorized)               | No           | Customizations on the denial status and other HTTP attributes when the request is unauthenticated. (Default: `401 Unauthorized`)   |
| `unauthorized`    | [Custom denial status spec](https://docs.kuadrant.io/latest/authorino/docs/features/#custom-denial-status-responseunauthenticated-and-responseunauthorized)               | No           | Customizations on the denial status and other HTTP attributes when the request is unauthorized. (Default: `403 Forbidden`)         |
| `success`         | [SuccessResponseSpec](#successresponsespec)                                                                                                                        | No           | Response items to be included in the auth response when the request is authenticated and authorized.                               |

##### SuccessResponseSpec

| **Field**         | **Type**                                                 | **Required** | **Description**                                                                                                                                   |
|-------------------|----------------------------------------------------------|:------------:|---------------------------------------------------------------------------------------------------------------------------------------------------|
| `headers`         | Map<String: [SuccessResponseItem](#successresponseitem)> | No           | Custom success response items wrapped as HTTP headers to be injected in the request.                                                              |
| `dynamicMetadata` | Map<String: [SuccessResponseItem](#successresponseitem)> | No           | Custom success response items wrapped as Envoy Dynamic Metadata. Use it to pass data along to other proxy filters, such as the rate-limit filter. |

###### SuccessResponseItem

| **Field**   | **Type**                                                                                                                                                             | **Required** | **Description**                                                                                                                                                               |
|-------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------|:------------:|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `plain`     | [Plain text response item](https://docs.kuadrant.io/latest/authorino/docs/features/#plain-text-responsesuccessheadersdynamicmetadataplain)                                  | No           | Plain text content. Use one of: `plain`, `json`, `wristband`.                                                                                                                 |
| `json`      | [JSON injection response item](https://docs.kuadrant.io/latest/authorino/docs/features/#json-injection-responsesuccessheadersdynamicmetadatajson)                           | No           | Specification of a JSON object. Use one of: `plain`, `json`, `wristband`.                                                                                                     |
| `wristband` | [Festival Wristband token response item](https://docs.kuadrant.io/latest/authorino/docs/features/#festival-wristband-tokens-responsesuccessheadersdynamicmetadatawristband) | No           | Specification of a JSON object. Use one of: `plain`, `json`, `wristband`.                                                                                                     |
| `key`       | String                                                                                                                                                               | No           | The key used to add the custom response item (name of the HTTP header or root property of the Dynamic Metadata object). Defaults to the name of the response item if omitted. |

#### CallbackRule

| **Field**        | **Type**                                                                                                       | **Required** | **Description**                                                 |
|------------------|----------------------------------------------------------------------------------------------------------------|:------------:|-----------------------------------------------------------------|
| `http`           | [HTTP endpoints callback spec](https://docs.kuadrant.io/latest/authorino/docs/features/#http-endpoints-callbackshttp) | No           | HTTP endpoint settings to build the callback request (webhook). |
| _(inline)_       | [AuthRuleCommon](#authrulecommon)                                                                              | No           |                                                                 |

### NamedPattern

| **Field**  | **Type** | **Required** | **Description**                                                                                                                                                                                                    |
|------------|----------|:------------:|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `selector` | String   | Yes          | A valid [Well-known attribute](https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md) whose resolved value in the data plane will be compared to `value`, using the `operator`.    |
| `operator` | String   | Yes          | The binary operator to be applied to the resolved value specified by the selector. One of: `eq` (equal to), `neq` (not equal to), `incl` (includes; for arrays), `excl` (excludes; for arrays), `matches` (regex). |
| `value`    | String   | Yes          | The static value to be compared to the one resolved from the selector.                                                                                                                                             |

## AuthPolicyStatus

| **Field**            | **Type**                          | **Description**                                                                                                                     |
|----------------------|-----------------------------------|-------------------------------------------------------------------------------------------------------------------------------------|
| `observedGeneration` | String                            | Number of the last observed generation of the resource. Use it to check if the status info is up to date with latest resource spec. |
| `conditions`         | [][ConditionSpec](#conditionspec) | List of conditions that define that status of the resource.                                                                         |

### ConditionSpec

* The *lastTransitionTime* field provides a timestamp for when the entity last transitioned from one status to another.
* The *message* field is a human-readable message indicating details about the transition.
* The *reason* field is a unique, one-word, CamelCase reason for the condition’s last transition.
* The *status* field is a string, with possible values **True**, **False**, and **Unknown**.
* The *type* field is a string with the following possible values:
  * Available: the resource has successfully configured;

| **Field**            | **Type**  | **Description**              |
|----------------------|-----------|------------------------------|
| `type`               | String    | Condition Type               |
| `status`             | String    | Status: True, False, Unknown |
| `reason`             | String    | Condition state reason       |
| `message`            | String    | Condition state description  |
| `lastTransitionTime` | Timestamp | Last transition timestamp    |
