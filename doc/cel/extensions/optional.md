# CEL Optional Type and Functions

The `optional` library provides a set of functions and macros for working with optional values in CEL. 
Optional values are useful for representing values that may or may not be present, similar to `Option` in Rust or `Optional` in Java.

## The `optional` Type

The CEL type for optional values is `optional_type(T)`, where `T` is the type of the value contained within the optional.

## Construction Functions

### optional.of

Creates a new optional containing the given value.

* **Signature:** `optional.of(T) -> optional_type(T)`

#### Examples:
```cel
optional.of(1)         // returns optional(1)
optional.of("hello")   // returns optional("hello")
optional.of(null)      // returns optional(null)
```

---

### optional.ofNonZeroValue

Creates a new optional containing the given value, or `optional.none()` if the value is a zero or empty value (e.g., `0`, `""`, `[]`, `{}`, `false`, or `null`).

* **Signature:** `optional.ofNonZeroValue(T) -> optional_type(T)`

#### Examples:
```cel
optional.ofNonZeroValue(null)    // returns optional.none()
optional.ofNonZeroValue("")      // returns optional.none()
optional.ofNonZeroValue(0)       // returns optional.none()
optional.ofNonZeroValue("hello") // returns optional.of("hello")
```

---

### optional.none

Returns a singleton value representing an empty optional.

* **Signature:** `optional.none() -> optional_type(any)`

#### Examples:
```cel
optional.none()
```

---

## Accessor Methods

### hasValue

Determines whether the optional contains a value.

* **Signature:** `<optional_type(T)>.hasValue() -> bool`

#### Examples:
```cel
optional.of(1).hasValue()   // returns true
optional.none().hasValue()  // returns false
```

---

### value

Obtains the value contained by the optional. If the optional is empty, it results in an error.

* **Signature:** `<optional_type(T)>.value() -> T`

#### Examples:
```cel
optional.of(1).value()   // returns 1
optional.none().value()  // error: optional.none() dereference
```

---

## Transformation Methods

### or

Chains optional expressions together, picking the first valued optional expression.

* **Signature:** `<optional_type(T)>.or(<optional_type(T)>) -> optional_type(T)`

#### Examples:
```cel
optional.none().or(optional.of(1)) // returns optional.of(1)
optional.of(1).or(optional.of(2))   // returns optional.of(1)
```

---

### orValue

Chains optional expressions together, picking the first valued optional or the default value.

* **Signature:** `<optional_type(T)>.orValue(T) -> T`

#### Examples:
```cel
optional.of(1).orValue(0)   // returns 1
optional.none().orValue(0)  // returns 0
{'a': 1}[? 'b'].orValue(0)  // returns 0
```

---

## Macros

### optMap

Performs a computation on the value if present and returns the result as an optional.

* **Signature:** `<optional_type(T)>.optMap(var, expr)`

#### Examples:
```cel
optional.of(1).optMap(i, i * 2)  // returns optional.of(2)
optional.none().optMap(i, i * 2) // returns optional.none()
```

---

### optFlatMap

Performs a computation on the value if present and produces an optional value within the computation.

* **Signature:** `<optional_type(T)>.optFlatMap(var, expr)`

#### Examples:
```cel
// m = {'key': {'subkey': 'value'}}
m.?key.optFlatMap(k, k.?subkey) // returns optional.of('value')

// m = {'key': {}}
m.?key.optFlatMap(k, k.?subkey) // returns optional.none()
```

---

## Selection and Indexing

### Optional Selection (`.?`)

If the field is present, creates an optional of the field value; otherwise, returns `optional.none()`.

#### Examples:
```cel
msg.?field                // optional.of(field) if present, else optional.none()
msg.?field.?nested_field  // optional.of(nested_field) if both are present
```

---

### Optional Indexing (`[? ]`)

If the index or key is present, creates an optional of the element value; otherwise, returns `optional.none()`.

#### Examples:
```cel
[1, 2, 3][? 1]    // returns optional.of(2)
[1, 2, 3][? 5]    // returns optional.none()
{'a': 1}[? 'a']   // returns optional.of(1)
{'a': 1}[? 'b']   // returns optional.none()
```

---

## List Functions (v2+)

### first / last

Returns the first or last value in a list as an optional.

* **Signature:** `<list<T>>.first() -> optional_type(T)`
* **Signature:** `<list<T>>.last() -> optional_type(T)`

#### Examples:
```cel
[1, 2, 3].first() // returns optional.of(1)
[].first()        // returns optional.none()
[1, 2, 3].last()  // returns optional.of(3)
```

---

### optional.unwrap / unwrapOpt

Converts a list of optional values to a list containing only values which are not `optional.none()`.

* **Signature:** `optional.unwrap(list<optional_type(T)>) -> list<T>`
* **Signature:** `<list<optional_type(T)>>.unwrapOpt() -> list<T>`

#### Examples:
```cel
optional.unwrap([optional.of(1), optional.none()]) // returns [1]
[optional.of(1), optional.none()].unwrapOpt()      // returns [1]
```
