# OIDCPolicy Extension

The _OIDCPolicy_ ([Open ID Connect](https://openid.net/developers/discover-openid-and-openid-connect/) Policy) extension
aims at helping users to easily & quickly get started with the Kuadrant stack to set up an
[OpenID Connect Authorization Code Flow](https://openid.net/specs/openid-connect-core-1_0.html#CodeFlowAuth) without needing to initially understand how to
use Kuadrant's `AuthPolicy` to achieve their goals.

## Overview

OIDCPolicy exposes in its config spec, the specifics for setting up an [OpenID Connect Authorization Code Flow](https://openid.net/specs/openid-connect-core-1_0.html#CodeFlowAuth)
leveraging behind the scenes the configuration of `AuthPolicies` and `HTTPRoutes` for the redirection to IDP login and authorization page,
token exchange via secure channel and other default protection rules for a given Application/Service.

## How it works

### Integration

OIDCPolicy works in conjunction with the [Extensions SDK](../overviews/extension-sdk.md), AuthPolicy and [Gateway API HTTPRoute](https://gateway-api.sigs.k8s.io/api-types/httproute/):

1. **OIDCPolicy** Entry point for provider configuration and authorization rules.
2. The policy automatically creates an `HTTPRoute` for the callback from the IDP and `AuthPolicies` for the protected rule and the callback one for token exchange
3. It also subscribes for Gateway updates through the Extensions SDK, in order to keep in sync with the AuthPolicies the hostname vlaue.
4. There is an extra **HTTPRoute** that was created by the OIDCPolicy reconciler is available to handle traffic for the code/token exchange
5. There is an **AuthPolicy** for Authenticating/Authorizing the access to the protected service and a second one attached to
the callback HTTPRoute that handles the code/token exchange.

### The OIDCPolicy custom resource

#### Overview

The `OIDCPolicy` spec includes the following parts:

- A reference to an existing Gateway API resource (`spec.targetRef`)
- Settings related to the Identity Provider (IDP) (`spec.provider`)
- A definition of settings that will enforce Authorization rules (`spec.auth`)

#### High-level example and field definition

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
    # Use it for automatically discovering the JWKS URL from an OpenID Connect Discovery endpoint (https://openid.net/specs/openid-connect-discovery-1_0.html).
    # The Well-Known Discovery path (i.e. "/.well-known/openid-configuration") is appended to this URL to fetch the OIDC configuration.
    issuerURL: https://idp-provider.example.org
  auth:
    # Claims contains the JWT Claims https://www.rfc-editor.org/rfc/rfc7519.html#section-4
    claims:
      group: kuadrant

```
## Using the OIDCPolicy
### Targeting a HTTPRoute networking resource
When a OIDCPolicy targets an HTTPRoute, the policy will be enforced on all traffic flowing through that specific route.
Target an HTTPRoute by setting the `spec.targetRef` field of the OIDCPolicy as follows:

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

### Provider definition
Definitions that will configure the IDP settings, the way it will authenticate with it, and the specific endpoints.

```yaml
    # Client/Application ID from the IDP
    clientID: oidc-demo
    # URL of the OpenID Connect (OIDC) token issuer endpoint. It's chosen when no _jwksURL_ is introduced
    issuerURL: https://idp-provider.example.org
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

### Auth definition
Definitions that will be used to get the [JWT](https://en.wikipedia.org/wiki/JSON_Web_Token) and enforce Authorization.

```yaml
    # TokenSource informs where the JWT token will be found in the request for authentication
    # At the moment it only supports `cookies` and it represents the name of the cookie, in the example below, _jwt_
    tokenSource: jwt
    # Claims contains the JWT Claims https://www.rfc-editor.org/rfc/rfc7519.html#section-4
    claims:
      # In this example is using the `groups_direct` claim with the value `engineering_team`
      groups_direct: "engineering_team"
```

## Prerequisites

Before using OIDCPolicy, ensure you have:

1. **Kuadrant Operator** installed and running
2. **Gateway API** resources (Gateway and HTTPRoute) configured
3. An **Identity Provider** such as [Keycloak](https://www.keycloak.org/) installed and running or an account somewhere

## Examples

Check out the following user guide for a complete example of using PlanPolicy:

- [OIDC with Keycloak](../user-guides/oidcpolicy/oidc-keycloak.md)
- [OIDC with Gitlab](../user-guides/oidcpolicy/oidc-gitlab.md)

## Known limitations

- The _OIDCPolicy_, at the moment, only targets  [GatewayAPI HTTPRoute](https://gateway-api.sigs.k8s.io/api-types/httproute/) objects
- It only implements the [OpenID Connect Authorization Code Flow](https://openid.net/specs/openid-connect-core-1_0.html#CodeFlowAuth) (recommended). Missing Implicit and Hybrid Flow
- Current OIDC Workflow works only for Browser apps and Native apps that manage the Auth via browser
- TokenSource works only with `cookies` as of today
