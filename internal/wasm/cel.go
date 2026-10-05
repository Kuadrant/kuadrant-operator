package wasm

import (
	"fmt"
	"strings"
)

func escapeCELString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

func joinPredicates(predicates []string, op string) string {
	switch len(predicates) {
	case 0:
		return "true"
	case 1:
		return predicates[0]
	default:
		parts := make([]string, len(predicates))
		for i, p := range predicates {
			parts[i] = fmt.Sprintf("(%s)", p)
		}
		return strings.Join(parts, fmt.Sprintf(" %s ", op))
	}
}

// CheckRequestCEL models an envoy.service.auth.v3.CheckRequest for CEL rendering.
type CheckRequestCEL struct {
	Scope           string
	MetadataContext MetadataCEL
}

func (r CheckRequestCEL) ToCEL() string {
	return fmt.Sprintf(`envoy.service.auth.v3.CheckRequest {
  attributes: envoy.service.auth.v3.AttributeContext {
    request: envoy.service.auth.v3.AttributeContext.Request {
      time: request.time,
      http: envoy.service.auth.v3.AttributeContext.HttpRequest {
        host: request.host,
        method: request.method,
        scheme: request.scheme,
        path: request.path,
        protocol: request.protocol,
        headers: request.headers
      }
    },
    destination: envoy.service.auth.v3.AttributeContext.Peer {
      address: envoy.config.core.v3.Address {
        socket_address: envoy.config.core.v3.SocketAddress {
          address: destination.address,
          port_value: uint(destination.port)
        }
      }
    },
    source: envoy.service.auth.v3.AttributeContext.Peer {
      address: envoy.config.core.v3.Address {
        socket_address: envoy.config.core.v3.SocketAddress {
          address: source.address,
          port_value: uint(source.port)
        }
      }
    },
    context_extensions: {"host": "%s"},
    metadata_context: %s
  }
}`, escapeCELString(r.Scope), r.MetadataContext.ToCEL())
}

// MetadataCEL models envoy.config.core.v3.Metadata with filter_metadata entries.
type MetadataCEL struct {
	FilterMetadata []FilterMetadataEntryCEL
}

func (m MetadataCEL) ToCEL() string {
	if len(m.FilterMetadata) == 0 {
		return "envoy.config.core.v3.Metadata{}"
	}
	entries := make([]string, len(m.FilterMetadata))
	for i, e := range m.FilterMetadata {
		entries[i] = e.ToCEL()
	}
	return fmt.Sprintf("envoy.config.core.v3.Metadata{filter_metadata: {%s}}", strings.Join(entries, ", "))
}

// FilterMetadataEntryCEL is a single domain entry within filter_metadata.
type FilterMetadataEntryCEL struct {
	Domain string
	Fields []MetadataFieldCEL
}

func (e FilterMetadataEntryCEL) ToCEL() string {
	fields := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		fields[i] = f.ToCEL()
	}
	return fmt.Sprintf(`"%s": google.protobuf.Struct{fields: {%s}}`,
		escapeCELString(e.Domain), strings.Join(fields, ", "))
}

// MetadataFieldCEL is a single field within a filter_metadata Struct.
// Auth-referencing expressions are wrapped as cel_expr for deferred evaluation;
// other expressions are resolved immediately as string values.
type MetadataFieldCEL struct {
	Key        string
	Expression string
}

func (f MetadataFieldCEL) ToCEL() string {
	return fmt.Sprintf(`"%s": %s`, escapeCELString(f.Key), f.valueCEL())
}

func (f MetadataFieldCEL) valueCEL() string {
	if strings.Contains(f.Expression, "auth.") {
		return fmt.Sprintf(
			`google.protobuf.Value{struct_value: google.protobuf.Struct{fields: {"cel_expr": google.protobuf.Value{string_value: "%s"}}}}`,
			escapeCELString(f.Expression),
		)
	}
	return fmt.Sprintf(`google.protobuf.Value{string_value: string(%s)}`, f.Expression)
}

// RateLimitRequestCEL models envoy.service.ratelimit.v3.RateLimitRequest for CEL rendering.
type RateLimitRequestCEL struct {
	Domain      string
	HitsAddend  string
	Descriptors []RateLimitDescriptorCEL
}

func (r RateLimitRequestCEL) ToCEL() string {
	descriptorsCEL := "[]"
	if len(r.Descriptors) > 0 {
		parts := make([]string, len(r.Descriptors))
		for i, d := range r.Descriptors {
			parts[i] = d.ToCEL()
		}
		descriptorsCEL = fmt.Sprintf("[%s]", strings.Join(parts, ", "))
	}
	return fmt.Sprintf(`envoy.service.ratelimit.v3.RateLimitRequest {
    domain: %s,
    hits_addend: %s,
    descriptors: %s
}`, r.Domain, r.HitsAddend, descriptorsCEL)
}

// ReserveRequestCEL models a kuadrant.service.ratelimit.v1.ReserveRequest for
// CEL rendering. Amount is required and expected to resolve to a uint. TTL is
// optional (a google.protobuf.Duration message): when empty the field is
// omitted so Limitador applies its own default; when set it is expected to
// resolve to a CEL duration (converted to google.protobuf.Duration by the
// wasm-shim).
type ReserveRequestCEL struct {
	Domain      string
	Amount      string
	TTL         string
	Descriptors []RateLimitDescriptorCEL
}

func (r ReserveRequestCEL) ToCEL() string {
	fields := []string{
		fmt.Sprintf("domain: %s", r.Domain),
		fmt.Sprintf("descriptors: %s", descriptorsToCEL(r.Descriptors)),
		fmt.Sprintf("amount: %s", r.Amount),
	}
	if r.TTL != "" {
		fields = append(fields, fmt.Sprintf("ttl: %s", r.TTL))
	}
	return fmt.Sprintf("kuadrant.service.ratelimit.v1.ReserveRequest {\n    %s\n}", strings.Join(fields, ",\n    "))
}

// CommitRequestCEL models a kuadrant.service.ratelimit.v1.CommitRequest for CEL
// rendering. ReservationID is the CEL expression that reads back the id captured
// by the paired Reserve action; ActualAmount is expected to resolve to a uint.
type CommitRequestCEL struct {
	Domain        string
	ReservationID string
	ActualAmount  string
	Descriptors   []RateLimitDescriptorCEL
}

func (r CommitRequestCEL) ToCEL() string {
	return fmt.Sprintf(`kuadrant.service.ratelimit.v1.CommitRequest {
    domain: %s,
    descriptors: %s,
    reservation_id: %s,
    actual_amount: %s
}`, r.Domain, descriptorsToCEL(r.Descriptors), r.ReservationID, r.ActualAmount)
}

// descriptorsToCEL renders a descriptor list as a CEL list literal.
func descriptorsToCEL(descriptors []RateLimitDescriptorCEL) string {
	if len(descriptors) == 0 {
		return "[]"
	}
	parts := make([]string, len(descriptors))
	for i, d := range descriptors {
		parts[i] = d.ToCEL()
	}
	return fmt.Sprintf("[%s]", strings.Join(parts, ", "))
}

// RateLimitDescriptorCEL models a single descriptor with entries.
//
// Two modes:
//   - Structured: Entries + optional Predicate. When Predicate is set, entries
//     are wrapped in a CEL ternary that evaluates to an empty list when false.
//   - Pre-rendered: EntriesExpr is set directly (for complex cases where
//     conditional and unconditional entry lists are concatenated with CEL "+").
type RateLimitDescriptorCEL struct {
	Entries     []DescriptorEntryCEL
	Predicate   string
	EntriesExpr string
}

func (d RateLimitDescriptorCEL) ToCEL() string {
	entriesCEL := d.EntriesExpr
	if entriesCEL == "" {
		entryParts := make([]string, len(d.Entries))
		for i, e := range d.Entries {
			entryParts[i] = e.ToCEL()
		}
		entriesCEL = fmt.Sprintf("[%s]", strings.Join(entryParts, ", "))

		if d.Predicate != "" {
			entriesCEL = fmt.Sprintf("((%s) ? %s : [])", d.Predicate, entriesCEL)
		}
	}

	return fmt.Sprintf("envoy.extensions.common.ratelimit.v3.RateLimitDescriptor { entries: %s }", entriesCEL)
}

func (d RateLimitDescriptorCEL) conditionalEntriesCEL() string {
	entryParts := make([]string, len(d.Entries))
	for i, e := range d.Entries {
		entryParts[i] = e.ToCEL()
	}
	entriesCEL := fmt.Sprintf("[%s]", strings.Join(entryParts, ", "))
	if d.Predicate != "" {
		return fmt.Sprintf("((%s) ? %s : [])", d.Predicate, entriesCEL)
	}
	return entriesCEL
}

// DescriptorEntryCEL models a single key/value entry within a descriptor.
type DescriptorEntryCEL struct {
	Key      string
	ValueCEL string
}

func (e DescriptorEntryCEL) ToCEL() string {
	return fmt.Sprintf(
		`envoy.extensions.common.ratelimit.v3.RateLimitDescriptor.Entry { key: "%s", value: %s }`,
		escapeCELString(e.Key), e.ValueCEL,
	)
}
