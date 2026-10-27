# OIDCPolicy extension

The _OIDCPolicy_ ([OpenID Connect](https://openid.net/developers/discover-openid-and-openid-connect/) Policy) extension helps users quickly get started with the Kuadrant stack to set up an [OpenID Connect Authorization Code Flow](https://openid.net/specs/openid-connect-core-1_0.html#CodeFlowAuth) without needing to understand how to use Kuadrant's `AuthPolicy`.

## Overview

The `OIDCPolicy` resource configures an [OpenID Connect Authorization Code Flow](https://openid.net/specs/openid-connect-core-1_0.html#CodeFlowAuth). Behind the scenes, it manages `AuthPolicies` and `HTTPRoutes` for IDP login redirect, authorization pages, token exchange over secure channels, and default protection rules for an application or service.

## Understand how OIDCPolicy works

### Review extension integration

OIDCPolicy works in conjunction with the [Extensions SDK](authoring-extensions.md), AuthPolicy, and [Gateway API HTTPRoute](https://gateway-api.sigs.k8s.io/api-types/httproute/):

1. **OIDCPolicy** acts as the entry point for provider configuration and authorization rules.
2. The policy automatically creates an `HTTPRoute` for the callback from the IDP and `AuthPolicies` for the protected rule and the callback for token exchange.
3. The policy subscribes to Gateway updates through the Extensions SDK to synchronize the hostname value with `AuthPolicies`.
4. The reconciler creates an additional **HTTPRoute** to handle traffic for the code and token exchange.
5. The policy manages two **AuthPolicy** resources: one authenticates and authorizes access to the protected service, and the second attaches to the callback `HTTPRoute` to handle the code and token exchange.

### Review the OIDCPolicy custom resource

The `OIDCPolicy` spec includes the following parts:

- A reference to an existing Gateway API resource (`spec.targetRef`)
- Settings related to the Identity Provider (IDP) (`spec.provider`)
- A definition of settings that enforce authorization rules (`spec.auth`)

#### Inspect field definitions and example configuration

```yaml
apiVersion: extensions.kuadrant.io/v1alpha1
kind: OIDCPolicy
metadata:
  name: my-oidc-policy
spec:
  # Reference to an existing networking resource to attach the policy to
  # Can target HTTPRoute resources
  # Must be in the same namespace as the OIDCPolicy
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: my-route
  provider:
    # OAuth2 Client ID.
    clientID: oidc-demo
    # URL of the OpenID Connect (OIDC) token issuer endpoint.
    # Use it for automatically discovering the JWKS URL from an OpenID Connect Discovery endpoint ([https://openid.net/specs/openid-connect-discovery-1_0.html](https://openid.net/specs/openid-connect-discovery-1_0.html)).
    # The Well-Known Discovery path (i.e. "/.well-known/openid-configuration") is appended to this URL to fetch the OIDC configuration.
    issuerURL: https://idp-provider.example.org
  auth:
    # Claims contains the JWT Claims [https://www.rfc-editor.org/rfc/rfc7519.html#section-4](https://www.rfc-editor.org/rfc/rfc7519.html#section-4)
    claims:
      group: kuadrant
```

## Use the OIDCPolicy

### Target an HTTPRoute resource

When an `OIDCPolicy` targets an `HTTPRoute`, the policy is enforced on all traffic flowing through that specific route. Target an `HTTPRoute` by setting the `spec.targetRef` field of the `OIDCPolicy`:

```yaml
apiVersion: extensions.kuadrant.io/v1alpha1
kind: OIDCPolicy
metadata:
  name: my-oidc-policy
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: my-route
  provider:
    # ... provider definition
  auth:
    # ... auth definition
```

### Configure the provider definition

Configure the IDP settings, authentication mechanisms, and specific endpoints:

```yaml
    # Client/Application ID from the IDP
    clientID: oidc-demo
    # URL of the OpenID Connect (OIDC) token issuer endpoint. It's chosen when no _jwksURL_ is introduced
    issuerURL: [https://idp-provider.example.org](https://idp-provider.example.org)
    # URL of the OIDC JSON Web Key Set (JWKS) endpoint. It's chosen when no _issuerURL_ is introduced
    jwksURL: https://idp-provider.example.org/openid-connect/jwks
    # Reference to a Kubernetes secret that will store the IDP client secret
    clientSecretRef:
      # The name of the secret in the Kuadrant's namespace to select from
      name: idp-secret
      # The key of the secret to select from. Must be a valid secret key
      key: secret
    # The full URL of the Authorization endpoints. Default value is the IssuerURL + "/oauth/authorize"
    authorizationEndpoint: https://idp-provider/openid-connect/auth
    # Defines the URL to obtain an Access Token, an ID Token, and optionally a Refresh Token. Default value is the IssuerURL + "/oauth/token"
    tokenEndpoint: https://idp-provider/openid-connect/token
    # The RedirectURI defines the URL that is part of the authentication request to the AuthorizationEndpoint and the one defined in the IDP. Default value is the IssuerURL + "/auth/callback"
    redirectURI: https://your-deployed-app.org/callback
```

### Configure the authentication definition

Configure parameters used to extract the [JWT](https://en.wikipedia.org/wiki/JSON_Web_Token) and enforce authorization:

```yaml
    # TokenSource informs where the JWT token will be found in the request for authentication
    # At the moment it only supports `cookies` and it represents the name of the cookie, in the example below, _jwt_
    tokenSource: jwt
    # Claims contains the JWT Claims [https://www.rfc-editor.org/rfc/rfc7519.html#section-4](https://www.rfc-editor.org/rfc/rfc7519.html#section-4)
    claims:
      # In this example is using the `groups_direct` claim with the value `engineering_team`
      groups_direct: "engineering_team"
```

## Prerequisites

Before using `OIDCPolicy`, satisfy the following requirements:

1. Install and run the **Kuadrant Operator**.
2. Configure **Gateway API** resources (`Gateway` and `HTTPRoute`).
3. Deploy an **Identity Provider** such as [Keycloak](https://www.keycloak.org/) or obtain access to an external provider account.

## Additional resources

Check out the following user guides for complete deployment examples:

- [OIDC with Keycloak](../user-guides/oidcpolicy/oidc-keycloak.md)
- [OIDC with Gitlab](../user-guides/oidcpolicy/oidc-gitlab.md)

## Known limitations

- `OIDCPolicy` currently only targets [Gateway API HTTPRoute](https://gateway-api.sigs.k8s.io/api-types/httproute/) objects.
- The policy only supports the [OpenID Connect Authorization Code Flow](https://openid.net/specs/openid-connect-core-1_0.html#CodeFlowAuth) (Implicit and Hybrid Flows are not supported).
- Current OIDC workflows only function for browser applications and native applications that manage authentication using a browser.
- `TokenSource` only supports `cookies` as a source.