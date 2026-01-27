CEL Kuadrant

# Introduction to CEL

## 1. The Basic Syntax

Using CEL in Kuadrant, you evaluate the **Request** (attributes like path, method, headers) or the **Connection** (mTLS details, source IP).

### Variables and Attribute Access

Kuadrant exposes a standard set of attributes. You access them using dot notation or map lookups.

* **Dot Notation:** `request.path`, `request.method`
* **Map Lookup:** `request.headers['user-agent']` (Required for headers, as they contain hyphens).

### Literals

CEL supports standard data types:

| Type | Examples |
| --- | --- |
| **Int** | `200`, `404`, `-1` |
| **UInt** | `200u`, `404u` |
| **String** | `'GET'`, `"/api/v1"`, `r"regex\d+"` |
| **Bool** | `true`, `false` |
| **Duration** | `duration('500ms')`, `duration('10s')` |
| **Map** | `{'group': 'admin', 'tier': 'gold'}` |
| **List** | `[1, 2, 3]`

Note: CEL has no implicit type coercion.
---

## 2. Logical Operators

Within policies, in `Predicate`s you  can combine checks. If the expression evaluates to `true`, the policy applies (e.g. allowing or denying the request based on the action).

### Boolean Logic

* **AND (`&&`):** Both conditions must be true.
```
// Method is POST AND path starts with /api/admin
request.method == 'POST' && request.path.startsWith('/api/admin')

```


* **OR (`||`):** At least one condition must be true.
```
// Method is GET OR HEAD
request.method == 'GET' || request.method == 'HEAD'

```


* **NOT (`!`):** Inverts the result.
```
// User-Agent must NOT contain 'bot'
!request.headers['user-agent'].contains('bot')

```



### Conditional Logic (If/Else)

Conditional logic is useful for dependent checks, such as validating specific headers only for certain paths.

```
// If path is /secure, check for x-user-id header, otherwise allow.
request.path.startsWith('/secure') ? has(request.headers['x-user-id']) : true

```

---

## 3. Handling Optional Fields (Presence)

In HTTP traffic, headers and metadata are often missing. Accessing a missing map key in CEL can result in an error or `no_such_field`.

### The `has()` Macro

Use `has()` to check if a header or metadata field exists before accessing it.

```proto
// Rule: If an Authorization header exists, it must start with 'Bearer'
has(request.headers['authorization']) ? request.headers['authorization'].startsWith('Bearer ') : true

```

> **Note:** For `request.headers`, checking `has()` ensures the key exists in the map. For standard attributes like `request.referer`, it checks if the value is populated.
> **Note:** See below to learn about the optional syntax, which can in places be an alternative to the ternary operator.

---

## 4. Working with Lists (SANs, JWT Claims)

While standard HTTP headers are often strings, Kuadrant provides powerful lists in some contexts like **JWT Auth** (Claims).

### `.all()`

Checks if **every** item in the list satisfies a condition.

```
auth.identity.groups.all(group, group.endsWith('.admin'))

```

### `.exists()`

Checks if **at least one** item satisfies a condition.

```
// Rule: The JWT 'groups' claim must contain 'admin'
auth.identity.claims['groups'].exists(g, g == 'admin')

```

### `.exists_one()`

Checks if **exactly one** item satisfies the condition.

```
// Rule: Exactly one group must match
auth.identity.groups.exists_one(group, group == 'foo')

```

---

## 5. String Manipulation & Regex

Validating paths and headers.

### Comparisons

* **Equality:** `request.method == 'PUT'`
* **Prefix/Suffix:**
```
request.path.startsWith('/public/')
request.headers['host'].endsWith('.example.com')

```


* **Contains:** `request.headers['user-agent'].contains('Mozilla')`

### Regular Expressions

CEL uses **RE2** syntax for regex.

```proto
// Rule: X-Request-ID must be a UUID-like format
request.headers['x-request-id'].matches(r'^[0-9a-f-]+$')

```

> **Tip:** Always use `r'...'` for regex strings to handle backslashes correctly.

---

## 6. Type Conversion & Math

HTTP headers are always strings. To compare them numerically (e.g., Content-Length or custom logic), you must cast them.

### Casting

* **int()**: Converts strings to integers.
* **size()**: Returns the size of a string, list, or map.

```
// Rule: Content-Length must be less than 1MB (1,000,000 bytes)
has(request.headers['content-length']) && int(request.headers['content-length']) < 1000000

```

### Timestamps and Durations

TODO!

---

## 7. The `Optional` Type

The `optional` type offers a cleaner way to handle missing headers or metadata without verbose `has()` checks.

### Creating Optionals

You can wrap values that might be missing:

* **`optional.of(value)`**: Wraps a value.
* **`optional.none()`**: Represents a missing value.

```
// Wraps the 'x-priority' header into an Optional
optional.of(request.headers['x-priority'])

```

### Unwrapping with Defaults (`orValue`)

Provide a default value if the header is missing.

**Old Way (Verbose):**

```
(has(request.headers['x-retries']) ? int(request.headers['x-retries']) : 0) < 3

```

**New Way (Optional):**

```
// If header is missing, default to 0, then check if < 3
optional.of(request.headers['x-retries']).orValue('0').matches(r'^[0-2]$')

```

*(Note: Since headers are strings, we handle the value as a string or cast inside a map).*

**Optional syntax:**

```
request.?headers[?'x-retries']).orValue('0').matches(r'^[0-2]$')

```

TODO explain the two forms

### Safe Transformation (`optMap`)

Transform a value only if it exists.

```
// Rule: If 'x-debug' header exists, it must be 'true'. If missing, pass.
optional.of(request.headers['x-debug'])
  .optMap(val, val == 'true')
  .orValue(true)

```

### Chaining (`or`)

Check multiple headers in order of preference.

```
// Use 'x-client-id', or fallback to 'x-app-id', or default to 'anonymous'
optional.of(request.headers['x-client-id'])
  .or(optional.of(request.headers['x-app-id']))
  .orValue('anonymous')

```

---

## Summary Cheat Sheet

| Requirement | CEL Expression Strategy |
| --- | --- |
| **Header must exist** | `has(request.headers['x-token'])` |
| **Path validation** | `request.path.startsWith('/api/')` |
| **Regex Match** | `request.headers['authority'].matches(r'.*\.internal$')` |
| **Size Limit** | `request.size < 1024` |
| **Header Fallback** | `request.headers[?'x-group'].orValue('guest')` |
