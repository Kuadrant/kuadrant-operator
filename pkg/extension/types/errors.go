package types

import "errors"

// ErrUpstreamUnreachable is returned by RegisterUpstreamMethod when the
// operator cannot establish a gRPC connection to the provided URL.
var ErrUpstreamUnreachable = errors.New("upstream unreachable")
