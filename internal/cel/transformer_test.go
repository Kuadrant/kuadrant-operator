package transformer

import (
	"testing"
)

func TestTransformNoReplaces(t *testing.T) {
	exp := "foobar('test', 'otherthingy')"
	if out, err := TransformCounterVariable(exp); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `foobar('test', 'otherthingy')` {
			t.Errorf(`This must not be transformed: %s`, *out)
		}
	}
}

func TestTransformWithStraightReplaces(t *testing.T) {
	exp := `test`
	if out, err := TransformCounterVariable(exp); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `descriptors[0].test` {
			t.Errorf(`Not transformed as expected: %s`, *out)
		}
	}
}

func TestTransformNoRecursiveReplaces(t *testing.T) {
	exp := `descriptors[0]["test"]`
	if out, err := TransformCounterVariable(exp); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `descriptors[0]["test"]` {
			t.Errorf(`Not transformed as expected: %s`, *out)
		}
	}
}

func TestTransformWithComplexReplaces(t *testing.T) {
	exp := `foobar(test, other.thingy, "foo") + foo(descriptors[0].test)`
	if out, err := TransformCounterVariable(exp); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `foobar(descriptors[0].test, descriptors[0].other.thingy, "foo") + foo(descriptors[0].test)` {
			t.Errorf(`Not transformed as expected: %s`, *out)
		}
	}
}

func TestTransformWithListReplaces(t *testing.T) {
	exp := `[test, other.thingy, "foo", foo(descriptors[0].test)]`
	if out, err := TransformCounterVariable(exp); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `[descriptors[0].test, descriptors[0].other.thingy, "foo", foo(descriptors[0].test)]` {
			t.Errorf(`Not transformed as expected: %s`, *out)
		}
	}
}

func TestTransformWithMapReplaces(t *testing.T) {
	exp := `{test: other.thingy, "foo": foo(descriptors[0].test)}`
	if out, err := TransformCounterVariable(exp); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `{descriptors[0].test: descriptors[0].other.thingy, "foo": foo(descriptors[0].test)}` {
			t.Errorf(`Not transformed as expected: %s`, *out)
		}
	}
}

func TestTransformWithMapLookupReplaces(t *testing.T) {
	exp := `other[thingy]`
	if out, err := TransformCounterVariable(exp); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `descriptors[0].other[descriptors[0].thingy]` {
			t.Errorf(`Not transformed as expected: %s`, *out)
		}
	}
}

func TestTransformMultiLines(t *testing.T) {
	exp := `{
test: other.thingy, 
"foo": foo(descriptors[0].test),
"bar": [test, other.thingy, "foo", foo(descriptors[0].test)],
}
`
	if out, err := TransformCounterVariable(exp); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `{
descriptors[0].test: descriptors[0].other.thingy, 
"foo": foo(descriptors[0].test),
"bar": [descriptors[0].test, descriptors[0].other.thingy, "foo", foo(descriptors[0].test)],
}
` {
			t.Errorf(`Not transformed as expected: %s`, *out)
		}
	}
}

func TestTransformIdentDecl(t *testing.T) {
	exp := `
["gold", "silver", "bronze"]
    .filter(i, i in
        [{
        "gold": has(auth.identity) && auth.identity.email_verified && auth.identity.email.endsWith("@mydomain.com"),
        "silver": has(auth.identity),
        "bronze": true,
        }].map(m, m.filter(key, m[key]))[0])[0]`
	if out, err := TransformCounterVariable(exp); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `
["gold", "silver", "bronze"]
    .filter(i, i in
        [{
        "gold": has(descriptors[0].auth.identity) && descriptors[0].auth.identity.email_verified && descriptors[0].auth.identity.email.endsWith("@mydomain.com"),
        "silver": has(descriptors[0].auth.identity),
        "bronze": true,
        }].map(m, m.filter(key, m[key]))[0])[0]` {
			t.Errorf(`Not transformed as expected: %s`, *out)
		}
	}
}

func TestTransformErrsOutOnBadSyntax(t *testing.T) {
	if exp, err := TransformCounterVariable("[foobar"); err == nil {
		t.Errorf(`We expected to fail here! But got "%s"`, *exp)
	}
}
