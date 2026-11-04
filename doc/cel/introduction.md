# Introduction to CEL

## The basic syntax

Using CEL in Kuadrant, you evaluate the **request** (attributes such as path, method, and headers) or the **connection** (mTLS details and source IP).

### Variables and attribute access

Kuadrant exposes a standard set of attributes. You can access them using dot notation or map lookups.

- **Dot notation:** `request.path`, `request.method`
- **Map lookup:** `request.headers['user-agent']` (required for headers because they contain hyphens).

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
| **List** | `[1, 2, 3]` |

For more details on all member overloads for these types, see the [standard library documentation](standard.md).

> [!Caution]
> CEL does not support implicit type coercion. For example, comparing different types is invalid:
>
> `"3" != 3`

---

## Logical operators

Within policies, you can combine checks in `Predicate` expressions. If the expression evaluates to `true`, the policy applies, for example, allowing or denying the request based on the action.

### Boolean logic

- **AND (`&&`):** Both conditions must be true.

  ```cel
  // Method is POST AND path starts with /api/admin
  request.method == 'POST' && request.path.startsWith('/api/admin')
  ```

- **OR (`||`):** At least one condition must be true.

  ```cel
  // Method is GET OR HEAD
  request.method == 'GET' || request.method == 'HEAD'
  ```

- **NOT (`!`):** Inverts the result.

  ```cel
  // User-Agent must NOT contain 'bot'
  !request.headers['user-agent'].contains('bot')
  ```

### Conditional logic (if/else)

Conditional logic is useful for dependent checks, such as validating specific headers only for certain paths.

```cel
// If path is /secure, check for x-user-id header, otherwise allow.
request.path.startsWith('/secure') ? has(request.headers['x-user-id']) : true
```

---

## Handling optional fields (presence)

In HTTP traffic, headers and metadata are often missing. Accessing a missing map key in CEL can result in an error or `no_such_field`.

### The `has()` macro

Use `has()` to check whether a header or metadata field exists before accessing it.

```cel
// Rule: If an Authorization header exists, it must start with 'Bearer'
has(request.headers['authorization']) ? request.headers['authorization'].startsWith('Bearer ') : true
```

> [!Note]
> For `request.headers`, checking `has()` ensures the key exists in the map. For standard attributes such as `request.referer`, it checks whether the value is populated.
>
> See the section on optional syntax below for an alternative to the ternary operator in some cases.

---

## Working with lists and maps

While standard HTTP headers are often strings, Kuadrant provides lists in some contexts, such as JWT authentication claims.

In CEL, you can use these macros on both maps and lists to work with collections.

### `.all()`

Checks whether every item in the list satisfies a condition.

```cel
auth.identity.groups.all(group, group.endsWith('.admin'))
```

### `.exists()`

Checks whether at least one item satisfies a condition.

```cel
// Rule: The JWT 'groups' claim must contain 'admin'
auth.identity.claims['groups'].exists(g, g == 'admin')
```

### `.exists_one()`

Checks whether exactly one item satisfies the condition.

```cel
// Rule: Exactly one group must match
auth.identity.groups.exists_one(group, group == 'foo')
```

### `.filter()`

Returns a new list containing only the elements that satisfy the condition.

```cel
// Get all groups that end with '.admin'
auth.identity.groups.filter(group, group.endsWith('.admin'))
```

### `.map()`

Returns a new list in which each element has been transformed by the expression.

```cel
// Convert all group names to lowercase
auth.identity.groups.map(group, group.lowerascii())
```

---

## String manipulation and regex

Use string operations to validate paths and headers.

### Comparisons

- **Equality:** `request.method == 'PUT'`
- **Prefix and suffix:**

  ```cel
  request.path.startsWith('/public/')
  request.headers['host'].endsWith('.example.com')
  ```

- **Contains:** `request.headers['user-agent'].contains('Mozilla')`

### Regular expressions

CEL uses RE2 syntax for regular expressions.

```cel
// Rule: X-Request-ID must be a UUID-like format
request.headers['x-request-id'].matches(r'^[0-9a-f-]+$')
```

> [!Tip]
> Always use `r'...'` for regex strings to handle backslashes correctly.

---

## Type conversion and math

HTTP headers are always strings. To compare them numerically, for example, to validate `Content-Length` or implement custom logic, you must cast them.

### Casting

- **`int()`:** Converts strings to integers.
- **`size()`:** Returns the size of a string, list, or map.

```cel
// Rule: Content-Length must be less than 1 MB (1,000,000 bytes)
has(request.headers['content-length']) && int(request.headers['content-length']) < 1000000
```

### Timestamps and durations

You can create `timestamp` and `duration` values and use functions to operate on these types.

```cel
// Was the request made in the first 12 hours of 2025?
request.time - timestamp('2025-01-01T12:00:00Z') < duration('12h')
```

For more details on all member overloads for these types, see the [standard library documentation](standard.md).

---

## The `Optional` type

The `optional` type provides a cleaner way to handle missing headers or metadata without verbose `has()` checks.

### Creating optionals

You can wrap values that might be missing:

- **`optional.of(value)`:** Wraps a value.
- **`optional.none()`:** Represents a missing value.

```cel
// Wraps the 'x-priority' header into an Optional
optional.of(request.headers['x-priority'])
```

### Unwrapping with defaults (`orValue`)

Provide a default value if the header is missing.

**Old way (verbose):**

```cel
(has(request.headers['x-retries']) ? int(request.headers['x-retries']) : 0) < 3
```

**New way (optional):**

```cel
// If the header is missing, default to 0, then check whether it is less than 3
optional.of(request.headers['x-retries']).orValue('0').matches(r'^[0-2]$')
```

*Note: Because headers are strings, the value is handled as a string or cast inside a map.*

**Optional syntax:**

```cel
request.?headers[?'x-retries'].orValue('0').matches(r'^[0-2]$')
```

The `.?` operator prevents an error if the field, `headers` in this case, is absent. Instead, it returns an `Optional` representing `None`. If the field is present, in this case a `Map<String, String>`, the value is wrapped in an `Optional` containing the actual reference to the value.

The `[?<index>]` syntax works similarly for index accesses into a collection, whether it is a list or a map.

To use the value, access it using `.orValue()` and provide a default value for cases where the value is absent.

To read more about the `Optional` type, see the [Optional documentation](extensions/optional.md).

### Safe transformation (`optMap`)

Transform a value only if it exists.

```cel
// Rule: If 'x-debug' header exists, it must be 'true'. If missing, pass.
optional.of(request.headers['x-debug'])
    .optMap(val, val == 'true')
    .orValue(true)
```

### Chaining (`or`)

Check multiple headers in order of preference.

```cel
// Use 'x-client-id', or fall back to 'x-app-id', or default to 'anonymous'
optional.of(request.headers['x-client-id'])
    .or(optional.of(request.headers['x-app-id']))
    .orValue('anonymous')
```

---

## Summary cheat sheet

| Requirement | CEL expression strategy |
| --- | --- |
| **Header must exist** | `has(request.headers['x-token'])` |
| **Path validation** | `request.path.startsWith('/api/')` |
| **Regex match** | `request.headers['authority'].matches(r'.*\.internal$')` |
| **Size limit** | `request.size < 1024` |
| **Header fallback** | `request.headers[?'x-group'].orValue('guest')` |