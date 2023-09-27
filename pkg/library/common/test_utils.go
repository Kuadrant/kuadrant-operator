package common

type PolicyKindStub struct{}

var _ Referrer = &PolicyKindStub{}

func (tpk *PolicyKindStub) Kind() string {
	return "TestPolicy"
}

func (tpk *PolicyKindStub) BackReferenceAnnotationName() string {
	return "kuadrant.io/testpolicies"
}

func (tpk *PolicyKindStub) DirectReferenceAnnotationName() string {
	return "kuadrant.io/testpolicy"
}
