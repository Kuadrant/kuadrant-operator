# CEL Strings Extension Functions

The strings extension library provides a set of functions for string manipulation in CEL.

## Functions

### charAt

Returns the character at the given position. If the position is negative, or greater than the length of the string, the function will produce an error.

* **Supported version:** 0
* **Signature:** `<string>.charAt(<int>) -> <string>`

#### Examples:

```cel
'hello'.charAt(4)  // return 'o'
'hello'.charAt(5)  // return ''
'hello'.charAt(-1) // error
```

---

### format

Returns a new string with substitutions being performed, printf-style.

* **Supported version:** 1 (Updated at version 4)
* **Signature:** `<string>.format(<list>) -> <string>`

The valid formatting clauses are:

* `%s` - substitutes a string. This can also be used on bools, lists, maps, bytes, Duration and Timestamp, in addition to all numerical types (int, uint, and double).
* `%d` - substitutes an integer.
* `%f` - substitutes a double with fixed-point precision. The default precision is 6.
* `%e` - substitutes a double in scientific notation. The default precision is 6.
* `%b` - substitutes an integer with its equivalent binary string. Can also be used on bools.
* `%x` - substitutes an integer with its equivalent in hexadecimal, or if given a string or bytes, will output each character's equivalent in hexadecimal.
* `%X` - same as above, but with A-F capitalized.
* `%o` - substitutes an integer with its equivalent in octal.

#### Examples:

```js
"this is a string: %s\nand an integer: %d".format(["str", 42]) // returns "this is a string: str\nand an integer: 42"
"a double substituted with %%s: %s".format([64.2]) // returns "a double substituted with %s: 64.2"
"string type: %s".format([type(string)]) // returns "string type: string"
"timestamp: %s".format([timestamp("2023-02-03T23:31:20+00:00")]) // returns "timestamp: 2023-02-03T23:31:20Z"
"duration: %s".format([duration("1h45m47s")]) // returns "duration: 6347s"
"%f".format([3.14]) // returns "3.140000"
"scientific notation: %e".format([2.71828]) // returns "scientific notation: 2.718280\u202f\u00d7\u202f10\u2070\u2070"
"5 in binary: %b".format([5]) // returns "5 in binary; 101"
"26 in hex: %x".format([26]) // returns "26 in hex: 1a"
"26 in hex (uppercase): %X".format([26]) // returns "26 in hex (uppercase): 1A"
"30 in octal: %o".format([30]) // returns "30 in octal: 36"
"a map inside a list: %s".format([[1, 2, 3, {"a": "x", "b": "y", "c": "z"}]]) // returns "a map inside a list: [1, 2, 3, {"a":"x", "b":"y", "c":"d"}]"
"true bool: %s - false bool: %s\nbinary bool: %b".format([true, false, true]) // returns "true bool: true - false bool: false\nbinary bool: 1"
```

---

### indexOf

Returns the integer index of the first occurrence of the search string. If the search string is not found the function returns -1.

The function also accepts an optional position from which to begin the substring search. If the substring is the empty string, the index where the search starts is returned (zero or custom).

* **Supported version:** 0
* **Signature:** `<string>.indexOf(<string>) -> <int>` or `<string>.indexOf(<string>, <int>) -> <int>`

#### Examples:

```js
'hello mellow'.indexOf('')         // returns 0
'hello mellow'.indexOf('ello')     // returns 1
'hello mellow'.indexOf('jello')    // returns -1
'hello mellow'.indexOf('', 2)      // returns 2
'hello mellow'.indexOf('ello', 2)  // returns 7
'hello mellow'.indexOf('ello', 20) // returns -1
'hello mellow'.indexOf('ello', -1) // error
```

---

### join

Returns a new string where the elements of string list are concatenated. The function also accepts an optional separator which is placed between elements in the resulting string.

* **Supported version:** 2 (Initial version was 0, but it was moved to v2 for generic `Lister` support)
* **Signature:** `<list<string>>.join() -> <string>` or `<list<string>>.join(<string>) -> <string>`

#### Examples:

```js
['hello', 'mellow'].join() // returns 'hellomellow'
['hello', 'mellow'].join(' ') // returns 'hello mellow'
[].join() // returns ''
[].join('/') // returns ''
```

---

### lastIndexOf

Returns the integer index at the start of the last occurrence of the search string. If the search string is not found the function returns -1.

The function also accepts an optional position which represents the last index to be considered as the beginning of the substring match. If the substring is the empty string, the index where the search starts is returned (string length or custom).

* **Supported version:** 0
* **Signature:** `<string>.lastIndexOf(<string>) -> <int>` or `<string>.lastIndexOf(<string>, <int>) -> <int>`

#### Examples:

```js
'hello mellow'.lastIndexOf('')         // returns 12
'hello mellow'.lastIndexOf('ello')     // returns 7
'hello mellow'.lastIndexOf('jello')    // returns -1
'hello mellow'.lastIndexOf('ello', 6)  // returns 1
'hello mellow'.lastIndexOf('ello', 20) // returns -1
'hello mellow'.lastIndexOf('ello', -1) // error
```

---

### lowerAscii

Returns a new string where all ASCII characters are lower-cased. This function does not perform Unicode case-mapping for characters outside the ASCII range.

* **Supported version:** 0
* **Signature:** `<string>.lowerAscii() -> <string>`

#### Examples:

```js
'TacoCat'.lowerAscii()      // returns 'tacocat'
'TacoCÆt Xii'.lowerAscii()  // returns 'tacocÆt xii'
```

---

### strings.quote

Takes the given string and makes it safe to print (without any formatting due to escape sequences). If any invalid UTF-8 characters are encountered, they are replaced with \uFFFD.

* **Supported version:** 1
* **Signature:** `strings.quote(<string>) -> <string>`

#### Examples:

```js
strings.quote('single-quote with "double quote"') // returns '"single-quote with \"double quote\""'
strings.quote("two escape sequences \a\n") // returns '"two escape sequences \\a\\n"'
```

---

### replace

Returns a new string based on the target, which replaces the occurrences of a search string with a replacement string if present. The function accepts an optional limit on the number of substring replacements to be made.

When the replacement limit is 0, the result is the original string. When the limit is a negative number, the function behaves the same as replace all.

* **Supported version:** 0
* **Signature:** `<string>.replace(<string>, <string>) -> <string>` or `<string>.replace(<string>, <string>, <int>) -> <string>`

#### Examples:

```js
'hello hello'.replace('he', 'we')     // returns 'wello wello'
'hello hello'.replace('he', 'we', -1) // returns 'wello wello'
'hello hello'.replace('he', 'we', 1)  // returns 'wello hello'
'hello hello'.replace('he', 'we', 0)  // returns 'hello hello'
'hello hello'.replace('', '_')  // returns '_h_e_l_l_o_ _h_e_l_l_o_'
'hello hello'.replace('h', '')  // returns 'ello ello'
```

---

### split

Returns a list of strings split from the input by the given separator. The function accepts an optional argument specifying a limit on the number of substrings produced by the split.

When the split limit is 0, the result is an empty list. When the limit is 1, the result is the target string to split. When the limit is a negative number, the function behaves the same as split all.

* **Supported version:** 0
* **Signature:** `<string>.split(<string>) -> <list<string>>` or `<string>.split(<string>, <int>) -> <list<string>>`

#### Examples:

```js
'hello hello hello'.split(' ')     // returns ['hello', 'hello', 'hello']
'hello hello hello'.split(' ', 0)  // returns []
'hello hello hello'.split(' ', 1)  // returns ['hello hello hello']
'hello hello hello'.split(' ', 2)  // returns ['hello', 'hello hello']
'hello hello hello'.split(' ', -1) // returns ['hello', 'hello', 'hello']
```

---

### substring

Returns the substring given a numeric range corresponding to character positions. Optionally may omit the trailing range for a substring from a given character position until the end of a string.

Character offsets are 0-based with an inclusive start range and exclusive end range. It is an error to specify an end range that is lower than the start range, or for either the start or end index to be negative or exceed the string length.

* **Supported version:** 0
* **Signature:** `<string>.substring(<int>) -> <string>` or `<string>.substring(<int>, <int>) -> <string>`

#### Examples:

```js
'tacocat'.substring(4)    // returns 'cat'
'tacocat'.substring(0, 4) // returns 'taco'
'tacocat'.substring(-1)   // error
'tacocat'.substring(2, 1) // error
```

---

### trim

Returns a new string which removes the leading and trailing whitespace in the target string. The trim function uses the Unicode definition of whitespace which does not include the zero-width spaces.

* **Supported version:** 0
* **Signature:** `<string>.trim() -> <string>`

#### Examples:

```js
'  \ttrim\n    '.trim() // returns 'trim'
```

---

### upperAscii

Returns a new string where all ASCII characters are upper-cased. This function does not perform Unicode case-mapping for characters outside the ASCII range.

* **Supported version:** 0
* **Signature:** `<string>.upperAscii() -> <string>`

#### Examples:

```js
'TacoCat'.upperAscii()      // returns 'TACOCAT'
'TacoCÆt Xii'.upperAscii()  // returns 'TACOCÆT XII'
```

---

### reverse

Returns a new string whose characters are the same as the target string, only formatted in reverse order. This function relies on converting strings to rune arrays in order to reverse.

* **Supported version:** 3
* **Signature:** `<string>.reverse() -> <string>`

#### Examples:

```js
'gums'.reverse() // returns 'smug'
'John Smith'.reverse() // returns 'htimS nhoJ'
```
