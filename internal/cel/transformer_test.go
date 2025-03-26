package transformer

import (
	"testing"
)

func TestSafeToPrefix(t *testing.T) {
	if !SafeToSimplyPrefix("foobar") {
		t.Errorf("Should be safe to simply prefix")
	}
	if !SafeToSimplyPrefix("foobar[0]['foo'].bar") {
		t.Errorf("Should be safe to simply prefix")
	}
	if SafeToSimplyPrefix("descriptors") {
		t.Errorf("Should not be safe to simply prefix")
	}
	if SafeToSimplyPrefix("descriptors[0]['foo'].bar") {
		t.Errorf("Should not be safe to simply prefix")
	}
	if SafeToSimplyPrefix("!foobar") {
		t.Errorf("Should not be safe to simply prefix")
	}
	if SafeToSimplyPrefix("foobar(foo, bar)") {
		t.Errorf("Should not be safe to simply prefix")
	}
}
