package transformer

import (
	"testing"
)

func TestTransformNoReplaces(t *testing.T) {
	exp := "foobar('auth', 'request')"
	if out, err := TransformCounterVariable(exp, true); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `foobar('auth', 'request')` {
			t.Errorf(`This must not be transformed: %s`, *out)
		}
	}
}

func TestSimpleTransform(t *testing.T) {
	exp := "auth.identity.user"
	if out, err := TransformCounterVariable(exp, false); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `descriptors[0]["auth.identity.user"]` {
			t.Errorf(`Not transformed as expected: %s`, *out)
		}
	}
}

func TestTransformWithStraightReplaces(t *testing.T) {
	exp := `request`
	if out, err := TransformCounterVariable(exp, true); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `descriptors[0].request` {
			t.Errorf(`Not transformed as expected: %s`, *out)
		}
	}
}

func TestTransformNoRecursiveReplaces(t *testing.T) {
	exp := `descriptors[0]["test"]`
	if out, err := TransformCounterVariable(exp, true); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `descriptors[0]["test"]` {
			t.Errorf(`Not transformed as expected: %s`, *out)
		}
	}
}

func TestTransformWithComplexReplaces(t *testing.T) {
	exp := `foobar(request, auth.thingy, "foo") + foo(descriptors[0].test)`
	if out, err := TransformCounterVariable(exp, true); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `foobar(descriptors[0].request, descriptors[0].auth.thingy, "foo") + foo(descriptors[0].test)` {
			t.Errorf(`Not transformed as expected: %s`, *out)
		}
	}
}

func TestTransformWithListReplaces(t *testing.T) {
	exp := `[source, destination.thingy, "foo", foo(descriptors[0].test)]`
	if out, err := TransformCounterVariable(exp, true); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `[descriptors[0].source, descriptors[0].destination.thingy, "foo", foo(descriptors[0].test)]` {
			t.Errorf(`Not transformed as expected: %s`, *out)
		}
	}
}

func TestTransformWithMapReplaces(t *testing.T) {
	exp := `{test: source.thingy, "foo": foo(descriptors[0].test)}`
	if out, err := TransformCounterVariable(exp, true); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `{test: descriptors[0].source.thingy, "foo": foo(descriptors[0].test)}` {
			t.Errorf(`Not transformed as expected: %s`, *out)
		}
	}
}

func TestTransformWithMapLookupReplaces(t *testing.T) {
	exp := `source[destination.thingy]`
	if out, err := TransformCounterVariable(exp, true); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `descriptors[0].source[descriptors[0].destination.thingy]` {
			t.Errorf(`Not transformed as expected: %s`, *out)
		}
	}
}

func TestTransformMultiLines(t *testing.T) {
	exp := `{
source: request.thingy, 
"foo": foo(descriptors[0].test),
"bar": [request, other.thingy, "foo", foo(descriptors[0].test)],
}
`
	if out, err := TransformCounterVariable(exp, true); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `{
descriptors[0].source: descriptors[0].request.thingy, 
"foo": foo(descriptors[0].test),
"bar": [descriptors[0].request, other.thingy, "foo", foo(descriptors[0].test)],
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
	if out, err := TransformCounterVariable(exp, true); err != nil {
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

func TestTransformIdentDeclAsString(t *testing.T) {
	exp := `
['gold', 'silver', 'bronze']
`
	if out, err := TransformCounterVariable(exp, false); err != nil {
		t.Errorf(`err: %v`, err)
	} else {
		if *out != `descriptors[0]["['gold', 'silver', 'bronze']"]` {
			t.Errorf(`Not transformed as expected: %s`, *out)
		}
	}
}

func TestTransformErrsOutOnBadSyntax(t *testing.T) {
	if exp, err := TransformCounterVariable("[foobar", true); err == nil {
		t.Errorf(`We expected to fail here! But got "%s"`, *exp)
	}
}
