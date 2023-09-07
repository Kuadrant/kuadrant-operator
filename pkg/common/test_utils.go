package common

type PolicyKindStub struct{}

func (tpk *PolicyKindStub) Kind() string {
	return "TestPolicy"
}

func (tpk *PolicyKindStub) BackReferenceAnnotationName() string {
	return "kuadrant.io/testpolicies"
}
