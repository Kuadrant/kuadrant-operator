# CEL Standard Function Library

The standard library provides a set of core functions for CEL.

## Logical Operators

### Conditional (Ternary)

The ternary operator tests a boolean predicate and returns the left-hand side (truthy) expression if true, or the right-hand side (falsy) expression if false.

* **Signature:** `<bool> ? <A> : <A> -> <A>`

#### Examples:
```js
'hello'.contains('lo') ? 'hi' : 'bye' // 'hi'
32 % 3 == 0 ? 'divisible' : 'not divisible' // 'not divisible'
```

---

### Logical AND

Logically AND two boolean values. Errors and unknown values are valid inputs and will not halt evaluation.

* **Signature:** `<bool> && <bool> -> <bool>`

#### Examples:
```js
true && true   // true
true && false  // false
error && true  // error
error && false // false
```

---

### Logical OR

Logically OR two boolean values. Errors and unknown values are valid inputs and will not halt evaluation.

* **Signature:** `<bool> || <bool> -> <bool>`

#### Examples:
```js
true || false // true
false || false // false
error || true // true
error || error // error
```

---

### Logical NOT

Logically negate a boolean value.

* **Signature:** `!<bool> -> <bool>`

#### Examples:
```js
!true // false
!false // true
!error // error
```

---

## Equality Operators

### Equals

Compare two values of the same type for equality.

* **Signature:** `<A> == <A> -> bool`

#### Examples:
```js
1 == 1 // true
'hello' == 'world' // false
bytes('hello') == b'hello' // true
duration('1h') == duration('60m') // true
dyn(3.0) == 3 // true
```

---

### Not Equals

Compare two values of the same type for inequality.

* **Signature:** `<A> != <A> -> bool`

#### Examples:
```js
1 != 2     // true
"a" != "a" // false
3.0 != 3.1 // true
```

---

## Mathematical Operators

### Addition

Adds two numeric values or concatenates two strings, bytes, or lists.

* **Signature:**
    * `<int> + <int> -> <int>`
    * `<uint> + <uint> -> <uint>`
    * `<double> + <double> -> <double>`
    * `<string> + <string> -> <string>`
    * `<bytes> + <bytes> -> <bytes>`
    * `<list<A>> + <list<A>> -> <list<A>>`
    * `<duration> + <duration> -> <duration>`
    * `<duration> + <timestamp> -> <timestamp>`
    * `<timestamp> + <duration> -> <timestamp>`

#### Examples:
```js
1 + 2 // 3
22u + 33u // 55u
3.14 + 1.59 // 4.73
"Hello, " + "world!" // "Hello, world!"
b'hi' + bytes('ya') // b'hiya'
[1] + [2, 3] // [1, 2, 3]
duration('1m') + duration('1s') // duration('1m1s')
duration('24h') + timestamp('2023-01-01T00:00:00Z') // timestamp('2023-01-02T00:00:00Z')
timestamp('2023-01-01T00:00:00Z') + duration('24h1m2s') // timestamp('2023-01-02T00:01:02Z')
```

---

### Division

Divide two numbers.

* **Signature:**
    * `<int> / <int> -> <int>`
    * `<uint> / <uint> -> <uint>`
    * `<double> / <double> -> <double>`

#### Examples:
```js
10 / 2 // 5
42u / 2u // 21u
7.0 / 2.0 // 3.5
```

---

### Modulo

Compute the modulus of one integer into another.

* **Signature:**
    * `<int> % <int> -> <int>`
    * `<uint> % <uint> -> <uint>`

#### Examples:
```js
3 % 2 // 1
6u % 3u // 0u
```

---

### Multiplication

Multiply two numbers.

* **Signature:**
    * `<int> * <int> -> <int>`
    * `<uint> * <uint> -> <uint>`
    * `<double> * <double> -> <double>`

#### Examples:
```js
-2 * 6 // -12
13u * 3u // 39u
3.5 * 40.0 // 140.0
```

---

### Negation

Negate a numeric value.

* **Signature:** `-<T> -> <T>` (where T is int or double)

#### Examples:
```js
-(5) // -5
-(3.14) // -3.14
```

---

### Subtraction

Subtract two numbers, or two time-related values.

* **Signature:**
    * `<int> - <int> -> <int>`
    * `<uint> - <uint> -> <uint>`
    * `<double> - <double> -> <double>`
    * `<duration> - <duration> -> <duration>`
    * `<timestamp> - <duration> -> <timestamp>`
    * `<timestamp> - <timestamp> -> <duration>`

#### Examples:
```js
5 - 3 // 2
42u - 3u // 39u
10.5 - 2.0 // 8.5
duration('1m') - duration('1s') // duration('59s')
timestamp('2023-01-10T12:00:00Z') - duration('12h') // timestamp('2023-01-10T00:00:00Z')
timestamp('2023-01-10T12:00:00Z') - timestamp('2023-01-10T00:00:00Z') // duration('12h')
```

---

## Relational Operators

### Less Than

Compare two values and return true if the first value is less than the second.

* **Signature:** `<T> < <T> -> bool` (supports bool, int, uint, double, string, bytes, timestamp, duration, and mixed numeric comparisons)

#### Examples:
```js
false < true // true
-2 < 3 // true
1 < 2u // true
1u < 2u // true
1.1 < 1.2 // true
'a' < 'b' // true
b'hello' < b'world' // true
timestamp('2001-01-01T02:03:04Z') < timestamp('2002-02-02T02:03:04Z') // true
duration('1ms') < duration('1s') // true
```

---

### Less Than or Equal

Compare two values and return true if the first value is less than or equal to the second.

* **Signature:** `<T> <= <T> -> bool` (supports same types as Less Than)

#### Examples:
```js
'a' <= 'a' // true
1 <= 1.0 // true
```

---

### Greater Than

Compare two values and return true if the first value is greater than the second.

* **Signature:** `<T> > <T> -> bool` (supports same types as Less Than)

#### Examples:
```js
3 > -2 // true
'b' > 'a' // true
```

---

### Greater Than or Equal

Compare two values and return true if the first value is greater than or equal to the second.

* **Signature:** `<T> >= <T> -> bool` (supports same types as Less Than)

#### Examples:
```js
2u >= 1u // true
'b' >= 'a' // true
```

---

## Collections and Indexing

### Indexing

Select a value from a list by index, or value from a map by key.

* **Signature:**
    * `<list<A>>[<int>] -> <A>`
    * `<map<K, V>>[<K>] -> <V>`

#### Examples:
```js
[1, 2, 3][1] // 2
{'key': 'value'}['key'] // 'value'
```

---

### In

Test whether a value exists in a list, or a key exists in a map.

* **Signature:**
    * `<A> in <list<A>> -> bool`
    * `<K> in <map<K, V>> -> bool`

#### Examples:
```js
2 in [1, 2, 3] // true
'key1' in {'key1': 'value1', 'key2': 'value2'} // true
```

---

### size

Compute the size of a list or map, the number of characters in a string, or the number of bytes in a sequence.

* **Signature:**
    * `size(<T>) -> int`
    * `<T>.size() -> int`
      (where T is string, bytes, list, or map)

#### Examples:
```js
size('hello') // 5
'hello'.size() // 5
size([1, 2, 3]) // 3
{'a': 1, 'b': 2}.size() // 2
```

---

## Type Conversions

### type

Convert a value to its type identifier.

* **Signature:** `type(<A>) -> type`

#### Examples:
```js
type(1) // int
type('hello') // string
```

---

### bool

Convert a value to a boolean.

* **Signature:** `bool(<T>) -> bool` (T can be bool, string)

#### Examples:
```js
bool('true') // true
bool(true) // true
```

---

### bytes

Convert a value to bytes.

* **Signature:** `bytes(<T>) -> bytes` (T can be bytes, string)

#### Examples:
```js
bytes('hello') // b'hello'
```

---

### double

Convert a value to a double.

* **Signature:** `double(<T>) -> double` (T can be double, int, uint, string)

#### Examples:
```js
double(123) // 123.0
double('1.23') // 1.23
```

---

### duration

Convert a value to a duration.

* **Signature:** `duration(<T>) -> duration` (T can be duration, int (nanoseconds), string)

#### Examples:
```js
duration('1h2m3s') // duration('3723s')
```

---

### dyn

Indicate that the type is dynamic for type-checking purposes.

* **Signature:** `dyn(<A>) -> dyn`

#### Examples:
```js
dyn(1) // 1
```

---

### int

Convert a value to an int.

* **Signature:** `int(<T>) -> int` (T can be int, uint, double, string, duration (nanoseconds), timestamp (epoch seconds))

#### Examples:
```js
int(123u) // 123
int('123') // 123
int(duration('1s')) // 1000000000
int(timestamp('1970-01-01T00:00:01Z')) // 1
```

---

### string

Convert a value to a string.

* **Signature:** `string(<T>) -> string` (T can be any primitive type)

#### Examples:
```js
string(123) // '123'
string(true) // 'true'
string(timestamp('1970-01-01T00:00:00Z')) // '1970-01-01T00:00:00Z'
```

---

### timestamp

Convert a value to a timestamp.

* **Signature:** `timestamp(<T>) -> timestamp` (T can be timestamp, int (epoch seconds), string)

#### Examples:
```js
timestamp(1) // timestamp('1970-01-01T00:00:01Z')
timestamp('2025-01-01T12:34:56Z') // timestamp('2025-01-01T12:34:56Z')
```

---

### uint

Convert a value to a uint.

* **Signature:** `uint(<T>) -> uint` (T can be uint, int, double, string)

#### Examples:
```js
uint(123) // 123u
uint('123') // 123u
```

---

## String Functions

### contains

Test whether a string contains a substring.

* **Signature:** `<string>.contains(<string>) -> bool`

#### Examples:
```js
'hello world'.contains('o w') // true
```

---

### endsWith

Test whether a string ends with a substring suffix.

* **Signature:** `<string>.endsWith(<string>) -> bool`

#### Examples:
```js
'hello world'.endsWith('world') // true
```

---

### startsWith

Test whether a string starts with a substring prefix.

* **Signature:** `<string>.startsWith(<string>) -> bool`

#### Examples:
```js
'hello world'.startsWith('hello') // true
```

---

### matches

Test whether a string matches an RE2 regular expression.

* **Signature:**
    * `matches(<string>, <string>) -> bool`
    * `<string>.matches(<string>) -> bool`

#### Examples:
```js
matches('123-456', '^[0-9]+(-[0-9]+)?$') // true
'hello'.matches('^h.*o$') // true
```

---

## Date and Time Functions

All timestamp accessor methods support an optional IANA timezone string argument. If omitted, UTC is used.

### getFullYear

Get the 0-based full year from a timestamp.

* **Signature:**
    * `<timestamp>.getFullYear() -> int`
    * `<timestamp>.getFullYear(<string>) -> int`

#### Examples:
```js
timestamp('2023-07-14T10:30:45.123Z').getFullYear() // 2023
timestamp('2023-01-01T05:30:00Z').getFullYear('-08:00') // 2022
```

---

### getMonth

Get the 0-based month from a timestamp.

* **Signature:**
    * `<timestamp>.getMonth() -> int`
    * `<timestamp>.getMonth(<string>) -> int`

#### Examples:
```js
timestamp('2023-07-14T10:30:45.123Z').getMonth() // 6
```

---

### getDayOfYear

Get the 0-based day of the year from a timestamp.

* **Signature:**
    * `<timestamp>.getDayOfYear() -> int`
    * `<timestamp>.getDayOfYear(<string>) -> int`

---

### getDayOfMonth

Get the 0-based day of the month from a timestamp.

* **Signature:**
    * `<timestamp>.getDayOfMonth() -> int`
    * `<timestamp>.getDayOfMonth(<string>) -> int`

---

### getDate

Get the 1-based day of the month from a timestamp.

* **Signature:**
    * `<timestamp>.getDate() -> int`
    * `<timestamp>.getDate(<string>) -> int`

---

### getDayOfWeek

Get the 0-based day of the week from a timestamp.

* **Signature:**
    * `<timestamp>.getDayOfWeek() -> int`
    * `<timestamp>.getDayOfWeek(<string>) -> int`

---

### getHours

Get the hours portion from a timestamp, or convert a duration to hours.

* **Signature:**
    * `<timestamp>.getHours() -> int`
    * `<timestamp>.getHours(<string>) -> int`
    * `<duration>.getHours() -> int`

#### Examples:
```js
timestamp('2023-07-14T10:30:45.123Z').getHours() // 10
duration('3723s').getHours() // 1
```

---

### getMinutes

Get the minutes portion from a timestamp, or convert a duration to minutes.

* **Signature:**
    * `<timestamp>.getMinutes() -> int`
    * `<timestamp>.getMinutes(<string>) -> int`
    * `<duration>.getMinutes() -> int`

---

### getSeconds

Get the seconds portion from a timestamp, or convert a duration to seconds.

* **Signature:**
    * `<timestamp>.getSeconds() -> int`
    * `<timestamp>.getSeconds(<string>) -> int`
    * `<duration>.getSeconds() -> int`

---

### getMilliseconds

Get the milliseconds portion from a timestamp or duration.

* **Signature:**
    * `<timestamp>.getMilliseconds() -> int`
    * `<timestamp>.getMilliseconds(<string>) -> int`
    * `<duration>.getMilliseconds() -> int`
