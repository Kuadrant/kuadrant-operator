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
