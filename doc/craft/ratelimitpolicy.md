# Crafting A RateLimitPolicy
A detail explanation of the fields in the RateLimitPolicy is outlined here.
For a quick reference refer to [Reference](/kuadrant-operator/doc/reference/ratelimitpolicy/) section.

## apiVersion and kind
The latest version of the RateLimitPolicy is `kuadrant.io/v1beta2`.
With the kind being `RateLimitPolicy`.
```yaml
apiVersion: kuadrant.io/v1beta2
kind: RatelimitPplicy
```

## Metadata
When creating the RateLimitPolicy the policy must be crated in the same namespace as the resource being targeted.
```yaml
metadata:
    name: toystore
    namespace: default
```
Once created Kuadrant may add and manage additional annotations and labels.
For example:
```yaml
metadata:
    annotations:
        kuadrant.io/namespace: default
```

## Spec

### limits
`spec.limits` is a list of named limits, which describe what limits should be placed on a resource.
Many named limits are allowed.
Limit names do require to be valid yaml keys.
```yaml
spec:
    limits:
        limit_1:
            ...
        limit_2:
            ...
```

#### counters
`counters` defines additional rate limit counters based on context qualifiers and well known selectors.
The full list of well known attributes can be found [here](/architecture/rfcs/0002-well-known-attributes/#request-attributes).
All defined counters are ANDed together.
```yaml
spec: 
    limits:
        limit_1:
            counters:
                - request.url_path
```
The above example creates a new counter for every url_path that is requested.

A more concrete example of why creating new counters base off the well known attributes may relate to comments on social network.
It might be business policy to only allow users to update comments once pre day. 
The example limit is defined below.
Comments are updated at a URI like "/comment/<UUID\>".
```yaml
spec: 
    limits:
        comments_update:
            counters:
                - request.url_path
            rates:
                - duration: 1
                  limit: 1
                  unit: day
            routeSelectors:
                - hostnames:
                    - 'api.toystore.com'
                  matches:
                    - method: POST
                      path:
                        type: PathPrefix
                        value: /comment
```

#### rates
`rates` defines the limit of requests allowed in a time period.
This allows protecting resources over different time periods without the need for duplicating limits.
A `rate` requires three fields: `duration`, `limit` and `unit`. 

- `duration`: an integer value of how long the time period is, e.g. 10
- `limit`: an integer value of how request are allowed in the time period, e.g. 5
- `unit`: unit of time that the `duration` is over.
Possible values are second, minute, hour and day.

```yaml
spec:
    limits:
        limit_1:
            rates:
                - duration: 10
                  limit: 5
                  unit: second
                - duration: 1
                  limit: 1500
                  unit: hour
```
The above example restricts "limit_1" to 5 requests every 10 seconds or 1500 requests every hour.
1500 requests an hour is more restrictive than 5 requests every 10 seconds.

#### routeSelectors
`routeSelectors` defines the semantics for matching an HTTP request based on conditions.
Each `routeSelector` can contain a list of `hostnames` and `matches`.

- `hostnames`: defined limits apply to the hostnames.
Wild card hostnames are allowed, e.g. *.toystore.com, but IP address are not allowed.
- `matches`: refines the `routeSelector` to more targeted requests.
Many matches can be defined.
A `match` is a complex object, see [match](#match) for more detail.

```yaml
spec:
    limits:
        limit_1:
            routeSelectors:
                - hostnames:
                    - 'api.toystore.com'
                    - 'admin.toystore.com'
                    - '*.example.com'
                  matches:
                    - method: GET
                      path:
                        type: PathPrefix
                        value: /
```
The above example selects an number of hostnames to protect, and the `matches` refines the selection to GET requests on any URI that is prefix with `/`.

##### match
`matches` defines the predicate used to match requests to a given action.
Multiple match types are ANDed together, i.e. the match will evaluate to true only if all conditions are satisfied.
A match can be made up of 4 optional fields, `headers`, `method`, `path` and `queryParams`

- `headers`: a list of objects referencing HTTP request headers.
Formatted as follows: `{name: <header name>, value: <expected value> }`.
- `method`: targets the HTTP method of the request: i.e. GET, HEAD, POST, PUT, DELETE, CONNECT, OPTIONS, TRACE, PATCH
- `path`: an object that denotes the type of match and the path. 
Formatted as follows: `{type: <match type>, value: <URI>}`.
Examples of the `type` are: Exact, PathPrefix, RegularExpression.
The default behaviour if the `path` is not specified if `{type: PathPrefix, value: /}`.
- `queryParams`: describes how to select a HTTP route by matching HTTP query parameters.
The object is formatted like `{name: <param name>, value: <param value>, type: <Exact|RegularExpression>}`.
    - `name`: minimum length = 1, maximum length = 256
    - `value`: minimum length = 1, maximum length = 1024
    - `type`: optional, accepted values: Exact, RegularExpression.
    Default value: Exact

```yaml
spec:
    limits:
        limit_1:
            routeSelectors:
                  matches:
                    - method: GET
                      path:
                        type: PathPrefix
                        value: /admin
                      headers:
                        - name: "version"
                          value: "v1"
                      queryParam:
                        - name: "search"
                          value: "release"
```
The above example selects all HTTP requests that have a method of GET, and the URI starts with "/admin".
Also the request must have a header entry of `version:v1` and a queryParam of `search=release`.
All fields are optional, and the above example shows how complex of a match that can be crafted.

#### when
`when` defines semantics for matching an HTTP request based on conditions.
This should be used only when no match for the request can be created using [routeSelectors](#routeSelectors).
Three fields used are: operator, selector, value

- `operator`: binary operator to be applied to the content fetched from the selector.
Possible values are: "eq" (equal to), "neq" (not equal to).
- `selector`: defines one item from the well known attributes.
The full list of well known attributes can be found [here](/architecture/rfcs/0002-well-known-attributes/#request-attributes).
- `value`: reference for the comparison.
```yaml
spec:
    limits:
        limit_1:
            when:
                - operator: "eq"
                  selector: "request.useragent"
                  value: "curl/8.0.1"
```
The above example will match HTTP requests that are sent with the header HTTP_USER_AGENT set with a value of "curl/8.0.1". 

### targetRef
`spec.targetRef` tells Kuadrant what resource is being targeted by the RateLimitPolicy.
There are three required fields: `group`, `kind` and `name`.

- `group` will be assigned **gateway.networking.k8s.io**.
- `kind` can have a value of **HTTPRoute** or **Gateway**.
- `name` is the resource that is being targeted.

```yaml
spec:
    targetref:
        group: gateway.networking.k8s.io
        kind: HTTPRoute
        name: toystore
```
The above example targets the HTTPRoute named toystore.

## Reference Example
```yaml
apiVersion: kuadrant.io/v1beta2
kind: RatelimitPplicy
metadata:
    name: toystore
    namespace: default
spec: 
    limits:
        toystore:
            rates:
                - duration: 10
                  limit: 5
                  unit: secound
            routeSelectors:
                - host:
                    - '*.toystore.com'
                  matches:
                    - method: GET
                      path:
                        type: PathPrefix
                        value: /toy
        comments_update:
            counters:
                - request.url_path
            rates:
                - duration: 1
                  limit: 1
                  unit: day
            routeSelectors:
                - hostnames:
                    - 'api.toystore.com'
                  matches:
                    - method: POST
                      path:
                        type: PathPrefix
                        value: /comment
    targetref:
        group: gateway.networking.k8s.io
        kind: HTTPRoute
        name: toystore
```
The above example defines two sets of limits, `toystore` and `comments_update` which are targeting the HTTP route called toystore.
`toystore` limits every subdomain of toystore.com to 5 requests in a 10-second period that does a GET request on URI's starting with /toy.
`comments_update` creates a unique counter for all URI's that are prefixed with /comment and limits the POST requests to 1 per day. 
The counter also targets only the api subdomain of toystore.com.
