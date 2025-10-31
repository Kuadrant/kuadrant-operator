// Package ext exposes a CEL Library (CelExt) that registers Kuadrant
// specific functions and constants for use inside policy CEL expressions.
//
// The library introduces helper member functions on policy objects:
//
//	self.findGateways()       -> []*Gateway
//	self.findAuthPolicies()   -> []*Policy (AuthPolicy kind)
//	targetRef.findGateways()  -> []*Gateway
//
// These functions query the in‑memory DAG maintained by the extension service
// to discover related Gateways and Policies based on target references. The
// symbol __KUADRANT_VERSION is also exported so expressions can branch on the
// extension feature set (e.g. == "1_dev").
//
// The package is imported indirectly by using CelExt(DAG) as a cel.EnvOption.
// Extension authors generally do not need to interact with internals beyond
// supplying a DAG implementation.
package ext
