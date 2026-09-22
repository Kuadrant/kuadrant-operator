package wasm

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ActionSpec is a service-agnostic intermediate that collects policy-derived data
// before materialization into a concrete Action via Build().
type ActionSpec struct {
	ServiceName     string
	Scope           string
	Predicates      []string
	ConditionalData []ConditionalData
	Sources         []string
	Bindings        []DataBinding
	Execution       ExecutionMode

	// Reservation carries Reserve/Commit-only parameters (RFC 0021). Unlike
	// ratelimit.domain/hits_addend, these never need to be predicate-gated or
	// merged across limits -- RateLimitReserveServiceName/RateLimitCommitServiceName
	// are excluded from mergeableServices, so one ActionSpec always corresponds
	// to exactly one limit -- so a typed field is used instead of the
	// ConditionalData known-attr convention the mergeable rate-limit services use.
	Reservation *ReservationSpec
}

// ReservationSpec carries per-limit reservation parameters for Reserve/Commit
// actions (RFC 0021).
type ReservationSpec struct {
	// ID is a config-time per-limit identifier used to derive the filter-local
	// store path for the reservation_id (see reservationStorePath).
	ID string
	// Amount is the CEL expression evaluating to the number of tokens to
	// reserve on request arrival (uint).
	Amount string
	// TTL is the CEL expression evaluating to the reservation hold duration,
	// or "" to leave it unset so Limitador applies its own default.
	TTL string
	// ActualAmount is the CEL expression evaluating to the actual tokens
	// consumed, consumed by Commit (e.g. responseBodyJSON("/usage/total_tokens")).
	ActualAmount string
}

type DataBinding struct {
	Domain     string
	Field      string
	Expression string
}

// ActionSetSpec is the intermediate between policy processing and final Config construction.
type ActionSetSpec struct {
	Name                string
	RouteRuleConditions RouteRuleConditions
	Specs               []ActionSpec
	ExtensionActions    []Action
	SourceRoute         string
}

// HasAuthAccess checks whether any predicate or expression value references "auth."
func (s ActionSpec) HasAuthAccess() bool {
	for _, cd := range s.ConditionalData {
		for _, predicate := range cd.Predicates {
			if strings.Contains(predicate, "auth.") {
				return true
			}
		}
		for _, data := range cd.Data {
			switch val := data.Value.(type) {
			case *Static:
				continue
			case *Expression:
				if strings.Contains(val.ExpressionItem.Value, "auth.") {
					return true
				}
			}
		}
	}
	return false
}

const (
	authResponseVar      = "auth_response"
	rateLimitResponseVar = "ratelimit_response"
	reportResponseVar    = "report_response"
	reserveResponseVar   = "reserve_response"
	commitResponseVar    = "commit_response"

	AuthStorePath = "auth"
)

// IsGuard returns true if this spec produces a guard action (runs during request phase).
// Report and Commit both run in the response phase, so they are not guards.
func (s ActionSpec) IsGuard() bool {
	return s.ServiceName != RateLimitReportServiceName && s.ServiceName != RateLimitCommitServiceName
}

// ProducedStorePaths returns the store paths that this spec's onReply chain will produce.
func (s ActionSpec) ProducedStorePaths() []string {
	switch s.ServiceName {
	case AuthServiceName:
		return []string{AuthStorePath}
	default:
		return nil
	}
}

// Build materializes this ActionSpec into a concrete Action by dispatching on ServiceName.
func (s ActionSpec) Build() Action {
	switch s.ServiceName {
	case AuthServiceName:
		return s.buildAuth()
	case RateLimitServiceName, RateLimitCheckServiceName:
		return s.buildRateLimit(rateLimitResponseVar, true, "ratelimit")
	case RateLimitReportServiceName:
		return s.buildRateLimit(reportResponseVar, false, "ratelimit_report")
	case RateLimitReserveServiceName:
		return s.buildReserve()
	case RateLimitCommitServiceName:
		return s.buildCommit()
	default:
		return NewFailAction("true", fmt.Sprintf("unknown service: %s", s.ServiceName)).
			WithSources(s.Sources)
	}
}

// BuildActions materializes a slice of ActionSpecs into Actions.
// Body field references (responseBodyJSON/requestBodyJSON) across all specs are
// extracted into a single StoreAction per direction, prepended to the result.
func BuildActions(specs []ActionSpec) []Action {
	type refEntry struct {
		ref     bodyRef
		sources []string
	}

	// Collect all body refs across all specs, grouped by direction.
	// Key: direction ("response"/"request"), value: pointer -> refEntry
	byDirection := make(map[string]map[string]refEntry)

	for _, spec := range specs {
		for _, cd := range spec.ConditionalData {
			for _, item := range cd.Data {
				if expr, ok := item.Value.(*Expression); ok {
					for _, ref := range extractBodyRefs(expr.ExpressionItem.Value) {
						if byDirection[ref.Direction] == nil {
							byDirection[ref.Direction] = make(map[string]refEntry)
						}
						entry := byDirection[ref.Direction][ref.Pointer]
						entry.ref = ref
						entry.sources = appendUnique(entry.sources, spec.Sources...)
						byDirection[ref.Direction][ref.Pointer] = entry
					}
				}
			}
		}
		for _, b := range spec.Bindings {
			for _, ref := range extractBodyRefs(b.Expression) {
				if byDirection[ref.Direction] == nil {
					byDirection[ref.Direction] = make(map[string]refEntry)
				}
				entry := byDirection[ref.Direction][ref.Pointer]
				entry.ref = ref
				entry.sources = appendUnique(entry.sources, spec.Sources...)
				byDirection[ref.Direction][ref.Pointer] = entry
			}
		}
		if spec.Reservation != nil {
			for _, ref := range extractBodyRefs(spec.Reservation.ActualAmount) {
				if byDirection[ref.Direction] == nil {
					byDirection[ref.Direction] = make(map[string]refEntry)
				}
				entry := byDirection[ref.Direction][ref.Pointer]
				entry.ref = ref
				entry.sources = appendUnique(entry.sources, spec.Sources...)
				byDirection[ref.Direction][ref.Pointer] = entry
			}
		}
	}

	if len(byDirection) == 0 {
		actions := make([]Action, len(specs))
		for i, spec := range specs {
			actions[i] = spec.Build()
		}
		return actions
	}

	// Build store actions and a replacement map
	replacements := make(map[string]string) // original call -> store path
	var storeActions []Action

	for _, direction := range []string{"request", "response"} {
		fields, ok := byDirection[direction]
		if !ok {
			continue
		}

		pointers := sortedKeys(fields)

		// Detect leaf-name collisions: count how many pointers share each leaf field
		leafCount := make(map[string]int)
		for _, entry := range fields {
			leafCount[entry.ref.FieldName]++
		}

		// Build map expression: {"field1": bodyJSON("/path1"), "field2": bodyJSON("/path2")}
		var mapEntries []string
		var allSources []string
		for _, pointer := range pointers {
			entry := fields[pointer]
			mapKey := entry.ref.FieldName
			if leafCount[mapKey] > 1 {
				mapKey = sanitizePointer(entry.ref.Pointer)
			}
			mapEntries = append(mapEntries, fmt.Sprintf(`"%s": %s`, mapKey, entry.ref.Original))
			replacements[entry.ref.Original] = bodyRefStorePath(direction, mapKey)
			allSources = appendUnique(allSources, entry.sources...)
		}

		var storePath string
		if direction == "request" {
			storePath = requestBodyStorePath
		} else {
			storePath = responseBodyStorePath
		}

		mapExpr := fmt.Sprintf("{%s}", strings.Join(mapEntries, ", "))
		storeActions = append(storeActions,
			NewStoreAction("true", storePath, mapExpr).
				WithGuard(false).
				WithSources(allSources),
		)
	}

	// Replace body refs in all specs and build
	modified := make([]ActionSpec, len(specs))
	for i, spec := range specs {
		modified[i] = spec.replaceBodyRefs(replacements)
	}

	actions := make([]Action, 0, len(storeActions)+len(modified))
	actions = append(actions, storeActions...)
	for _, spec := range modified {
		actions = append(actions, spec.Build())
	}
	return actions
}

func (s ActionSpec) replaceBodyRefs(replacements map[string]string) ActionSpec {
	newCD := make([]ConditionalData, len(s.ConditionalData))
	for i, cd := range s.ConditionalData {
		newData := make([]DataType, len(cd.Data))
		for j, item := range cd.Data {
			if expr, ok := item.Value.(*Expression); ok {
				newValue := expr.ExpressionItem.Value
				for original, storePath := range replacements {
					newValue = strings.ReplaceAll(newValue, original, storePath)
				}
				if newValue != expr.ExpressionItem.Value {
					newData[j] = DataType{Value: &Expression{
						ExpressionItem: ExpressionItem{
							Key:   expr.ExpressionItem.Key,
							Value: newValue,
						},
					}}
				} else {
					newData[j] = item
				}
			} else {
				newData[j] = item
			}
		}
		newCD[i] = ConditionalData{Predicates: cd.Predicates, Data: newData}
	}

	newBindings := make([]DataBinding, len(s.Bindings))
	for i, b := range s.Bindings {
		newExpr := b.Expression
		for original, storePath := range replacements {
			newExpr = strings.ReplaceAll(newExpr, original, storePath)
		}
		newBindings[i] = DataBinding{Domain: b.Domain, Field: b.Field, Expression: newExpr}
	}

	newReservation := s.Reservation
	if s.Reservation != nil {
		newActualAmount := s.Reservation.ActualAmount
		for original, storePath := range replacements {
			newActualAmount = strings.ReplaceAll(newActualAmount, original, storePath)
		}
		if newActualAmount != s.Reservation.ActualAmount {
			r := *s.Reservation
			r.ActualAmount = newActualAmount
			newReservation = &r
		}
	}

	return ActionSpec{
		ServiceName:     s.ServiceName,
		Scope:           s.Scope,
		Predicates:      s.Predicates,
		ConditionalData: newCD,
		Sources:         s.Sources,
		Bindings:        newBindings,
		Execution:       s.Execution,
		Reservation:     newReservation,
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func appendUnique(dest []string, items ...string) []string {
	seen := make(map[string]bool, len(dest))
	for _, s := range dest {
		seen[s] = true
	}
	for _, item := range items {
		if !seen[item] {
			dest = append(dest, item)
			seen[item] = true
		}
	}
	return dest
}

func (s ActionSpec) buildAuth() *GrpcAction {
	request := CheckRequestCEL{
		Scope:           s.Scope,
		MetadataContext: buildMetadataContext(s.Bindings),
	}
	predicate := buildActionPredicate(s.Predicates)

	return NewGrpcAction(predicate, authResponseVar, s.ServiceName, request.ToCEL(), "auth").
		WithSources(s.Sources).
		WithOnReply(buildAuthOnReply(authResponseVar)...)
}

func (s ActionSpec) buildRateLimit(responseVar string, isGuard bool, label string) *GrpcAction {
	request := buildRateLimitRequest(s.Scope, s.ConditionalData, s.Bindings)
	predicate := buildRateLimitPredicate(s.Predicates, s.ConditionalData)

	var onReply []Action
	if isGuard {
		onReply = buildRateLimitOnReply(responseVar, s.ServiceName == RateLimitCheckServiceName)
	} else {
		onReply = buildReportOnReply(responseVar)
	}

	return NewGrpcAction(predicate, responseVar, s.ServiceName, request.ToCEL(), label).
		WithGuard(isGuard).
		WithExecution(s.Execution).
		WithSources(s.Sources).
		WithOnReply(onReply...)
}

// buildReserve materializes a Reserve action (RFC 0021): it reserves an estimated
// token amount on request arrival (guard phase) and, on reply, stashes the
// returned reservation_id at a per-limit filter-local store path for the paired
// Commit action to read back.
func (s ActionSpec) buildReserve() *GrpcAction {
	var id string
	if s.Reservation != nil {
		id = s.Reservation.ID
	}
	request := buildReserveRequest(s.Scope, s.ConditionalData, s.Bindings, s.Reservation)
	predicate := buildRateLimitPredicate(s.Predicates, s.ConditionalData)

	return NewGrpcAction(predicate, reserveResponseVar, s.ServiceName, request.ToCEL(), "ratelimit_reserve").
		WithGuard(true).
		WithExecution(s.Execution).
		WithSources(s.Sources).
		WithOnReply(buildReserveOnReply(reserveResponseVar, id)...)
}

// buildCommit materializes a Commit action (RFC 0021): on response it commits
// the actual token usage against the reservation captured by the paired
// Reserve action.
//
// There are two independent runtime facts: whether a reservation_id was
// stored (a Reserve succeeded and held capacity) and whether the actual token
// usage could be parsed from the upstream response. Commit is skipped
// entirely only when neither is true (e.g. Reserve failed open on a gRPC
// error/timeout AND the upstream response body doesn't carry a parseable
// usage figure - a connection issue or a non-JSON error body) - there would
// be nothing to release and nothing to report. Otherwise it always fires:
//   - reservation held, usage parseable: normal commit.
//   - reservation held, usage unparseable: commit with actual_amount 0, so
//     the hold is still released without charging for an unknown amount.
//   - no reservation, usage parseable: commit with an empty reservation_id,
//     which Limitador treats like a plain Report (RFC 0021).
func (s ActionSpec) buildCommit() *GrpcAction {
	var id, rawActualAmount string
	if s.Reservation != nil {
		id = s.Reservation.ID
		rawActualAmount = s.Reservation.ActualAmount
	}
	storePath := reservationStorePath(id)
	// != null rather than has(): wasm-shim's dynamic kuadrant.* map inserts a
	// null placeholder for any referenced-but-unstored leaf once a sibling
	// path exists anywhere under the shared root, which makes has() report
	// false positives for reservations that were never actually held.
	hasReservation := fmt.Sprintf("%s != null", storePath)

	amountKnown := "false"
	if rawActualAmount != "" {
		amountKnown = fmt.Sprintf("(%s) != null", rawActualAmount)
	}

	request := buildCommitRequest(s.Scope, s.ConditionalData, s.Bindings, storePath, hasReservation, rawActualAmount, amountKnown)
	predicate := andPredicate(
		buildRateLimitPredicate(s.Predicates, s.ConditionalData),
		fmt.Sprintf("(%s) || (%s)", hasReservation, amountKnown),
	)

	return NewGrpcAction(predicate, commitResponseVar, s.ServiceName, request.ToCEL(), "ratelimit_commit").
		WithGuard(false).
		WithExecution(s.Execution).
		WithSources(s.Sources).
		WithOnReply(buildCommitOnReply(commitResponseVar)...)
}

// --- Predicate helpers ---

func andPredicate(a, b string) string {
	if a == "true" {
		return b
	}
	if b == "true" {
		return a
	}
	return fmt.Sprintf("(%s) && (%s)", a, b)
}

func buildActionPredicate(predicates []string) string {
	return joinPredicates(predicates, "&&")
}

func buildDescriptorPredicate(conditionalData []ConditionalData) string {
	if len(conditionalData) == 0 {
		return "true"
	}
	for _, cd := range conditionalData {
		if len(cd.Predicates) == 0 {
			return "true"
		}
	}

	var blockPredicates []string
	for _, cd := range conditionalData {
		if len(cd.Predicates) == 1 {
			blockPredicates = append(blockPredicates, cd.Predicates[0])
		} else {
			blockPredicates = append(blockPredicates, fmt.Sprintf("(%s)", joinPredicates(cd.Predicates, "&&")))
		}
	}

	if len(blockPredicates) == 1 {
		return blockPredicates[0]
	}
	return strings.Join(blockPredicates, " || ")
}

func buildRateLimitPredicate(actionPredicates []string, conditionalData []ConditionalData) string {
	actionPred := buildActionPredicate(actionPredicates)
	conditionalPred := buildDescriptorPredicate(conditionalData)

	if actionPred == "true" && conditionalPred == "true" {
		return "true"
	}
	if actionPred == "true" {
		return conditionalPred
	}
	if conditionalPred == "true" {
		return actionPred
	}
	return fmt.Sprintf("(%s) && (%s)", actionPred, conditionalPred)
}

// --- Binding helpers ---

// DomainAndFieldName splits a binding key on the last "." into (domain, field).
// "auth.identity.user" → ("auth.identity", "user")
// "simple" → ("", "simple")
func DomainAndFieldName(name string) (string, string) {
	idx := strings.LastIndex(name, ".")
	if idx < 0 {
		return "", name
	}
	return name[:idx], name[idx+1:]
}

func isResponsePhaseExpression(expr string) bool {
	return strings.Contains(expr, "responseBodyJSON(") ||
		strings.Contains(expr, "requestBodyJSON(") ||
		strings.Contains(expr, responseBodyStorePath) ||
		strings.Contains(expr, requestBodyStorePath)
}

// AttachBindings walks specs in pipeline order and attaches only bindings whose
// dependencies are satisfied at each position. Two checks are applied:
//   - Store-path availability: specs declare produced store paths via
//     ProducedStorePaths(); bindings referencing a path not yet produced are excluded.
//   - Response-phase access: guard specs (request phase) cannot evaluate
//     response-body expressions, so those bindings are excluded.
func AttachBindings(specs []ActionSpec, bindings []DataBinding) {
	if len(bindings) == 0 {
		return
	}

	pendingPaths := make(map[string]bool)
	for _, spec := range specs {
		for _, path := range spec.ProducedStorePaths() {
			pendingPaths[path] = true
		}
	}

	for i := range specs {
		for _, path := range specs[i].ProducedStorePaths() {
			delete(pendingPaths, path)
		}

		pending := make([]string, 0, len(pendingPaths))
		for p := range pendingPaths {
			pending = append(pending, p)
		}

		specs[i].Bindings = append(specs[i].Bindings,
			availableBindings(bindings, pending, specs[i].IsGuard())...)
	}
}

func availableBindings(bindings []DataBinding, pendingPaths []string, guard bool) []DataBinding {
	if len(pendingPaths) == 0 && !guard {
		return bindings
	}
	var filtered []DataBinding
	for _, b := range bindings {
		if referencesPendingPath(b.Expression, pendingPaths) {
			continue
		}
		if guard && isResponsePhaseExpression(b.Expression) {
			continue
		}
		filtered = append(filtered, b)
	}
	return filtered
}

func referencesPendingPath(expr string, pendingPaths []string) bool {
	for _, path := range pendingPaths {
		if strings.Contains(expr, path+".") {
			return true
		}
	}
	return false
}

// bodyJSONPattern matches responseBodyJSON(...) and requestBodyJSON(...) calls, either with a single
// quoted JSON pointer argument (e.g. responseBodyJSON("/usage/total_tokens")) or with an ordered list of
// pointer candidates plus an optional type hint (e.g. responseBodyJSON(["/a", "/b"], "number")).
var bodyJSONPattern = regexp.MustCompile(`(response|request)BodyJSON\(\s*(\[[^\]]*\]|"[^"]*"|'[^']*')\s*(?:,\s*"([^"]*)")?\s*\)`)

// pointerListItemPattern extracts individual quoted string literals from within a list literal argument.
var pointerListItemPattern = regexp.MustCompile(`"([^"]*)"|'([^']*)'`)

const (
	responseBodyStorePath = "kuadrant.internal.response.body"
	requestBodyStorePath  = "kuadrant.internal.request.body"

	// reservationStorePathPrefix is the filter-local store-path namespace under
	// which a Reserve action stashes the reservation_id returned by the ratelimit
	// service, so its paired Commit action can read it back within the same
	// request. Like the body paths, it stays filter-local (no host export needed).
	// A per-limit segment is appended (see reservationStorePath) so concurrent
	// reservations for different limits do not clobber one another.
	reservationStorePathPrefix = "kuadrant.internal.tokenratelimit.reservation"
	// pointerListKeySep separates ordered pointer candidates (plus trailing type hint) when building the
	// canonical dedup/collision identity key for a list-form body ref.
	pointerListKeySep = "\x1f"
)

// reservationStorePath returns the per-limit store path for a reservation id.
// The id (a per-limit identifier shared by the paired Reserve and Commit specs)
// is sanitized so it forms a single trailing path segment.
func reservationStorePath(id string) string {
	return reservationStorePathPrefix + "." + strings.ReplaceAll(id, ".", "_")
}

type bodyRef struct {
	Original  string // the full matched call, e.g. responseBodyJSON("/usage/total_tokens")
	Direction string // "response" or "request"
	FieldName string // derived map key, e.g. "total_tokens"
	Pointer   string // identity key: the JSON pointer, or an ordered-list+type-hint canonical key
}

func bodyRefFieldName(jsonPointer string) string {
	segments := strings.Split(strings.TrimPrefix(jsonPointer, "/"), "/")
	return segments[len(segments)-1]
}

func sanitizePointer(pointer string) string {
	replacer := strings.NewReplacer("/", "_", pointerListKeySep, "_")
	return strings.Trim(replacer.Replace(strings.TrimPrefix(pointer, "/")), "_")
}

func bodyRefStorePath(direction, fieldName string) string {
	if direction == "request" {
		return requestBodyStorePath + "." + fieldName
	}
	return responseBodyStorePath + "." + fieldName
}

// parsePointerList extracts the ordered quoted string literals from a list literal argument,
// e.g. `["/a", "/b"]` -> ["/a", "/b"].
func parsePointerList(listArg string) []string {
	matches := pointerListItemPattern.FindAllStringSubmatch(listArg, -1)
	pointers := make([]string, 0, len(matches))
	for _, m := range matches {
		if strings.HasPrefix(m[0], `"`) {
			pointers = append(pointers, m[1])
		} else {
			pointers = append(pointers, m[2])
		}
	}
	return pointers
}

func extractBodyRefs(expr string) []bodyRef {
	matches := bodyJSONPattern.FindAllStringSubmatch(expr, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var refs []bodyRef
	for _, m := range matches {
		original := m[0]
		if seen[original] {
			continue
		}
		seen[original] = true

		arg, typeHint := m[2], m[3]

		var fieldName, pointer string
		if strings.HasPrefix(arg, "[") {
			pointers := parsePointerList(arg)
			if len(pointers) == 0 {
				continue
			}
			fieldName = bodyRefFieldName(pointers[0])
			pointer = strings.Join(pointers, pointerListKeySep) + pointerListKeySep + typeHint
		} else {
			p := strings.Trim(arg, `"'`)
			fieldName = bodyRefFieldName(p)
			pointer = p
		}

		refs = append(refs, bodyRef{
			Original:  original,
			Direction: m[1],
			FieldName: fieldName,
			Pointer:   pointer,
		})
	}
	return refs
}

// --- Auth message construction ---

func buildMetadataContext(bindings []DataBinding) MetadataCEL {
	if len(bindings) == 0 {
		return MetadataCEL{}
	}

	type domainGroup struct {
		domain string
		fields []MetadataFieldCEL
	}
	byDomain := make(map[string]*domainGroup)
	for _, b := range bindings {
		key := "io.kuadrant"
		if b.Domain != "" {
			key = fmt.Sprintf("io.kuadrant.%s", b.Domain)
		}
		if _, ok := byDomain[key]; !ok {
			byDomain[key] = &domainGroup{domain: key}
		}
		byDomain[key].fields = append(byDomain[key].fields, MetadataFieldCEL{
			Key:        b.Field,
			Expression: b.Expression,
		})
	}

	groups := make([]domainGroup, 0, len(byDomain))
	for _, g := range byDomain {
		groups = append(groups, *g)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].domain < groups[j].domain })

	entries := make([]FilterMetadataEntryCEL, len(groups))
	for i, g := range groups {
		entries[i] = FilterMetadataEntryCEL{
			Domain: g.domain,
			Fields: g.fields,
		}
	}
	return MetadataCEL{FilterMetadata: entries}
}

func buildAuthOnReply(name string) []Action {
	return []Action{
		NewDenyAction(
			fmt.Sprintf("has(%s.denied_response)", name),
			fmt.Sprintf(
				`DenyResponse{status: (%s.denied_response.status.code != 0) ? uint(%s.denied_response.status.code) : 403u, headers: %s.denied_response.headers, body: %s.denied_response.body}`,
				name, name, name, name,
			),
		),
		NewFailAction(
			fmt.Sprintf(
				"has(%s.ok_response) && (%s.ok_response.response_headers_to_add.size() > 0 || %s.ok_response.headers_to_remove.size() > 0 || %s.ok_response.query_parameters_to_set.size() > 0 || %s.ok_response.query_parameters_to_remove.size() > 0)",
				name, name, name, name, name,
			),
			"Unsupported field in OkHttpResponse",
		),
		NewStoreAction(
			fmt.Sprintf("has(%s.ok_response) && has(%s.dynamic_metadata)", name, name),
			AuthStorePath,
			fmt.Sprintf("%s.dynamic_metadata", name),
		).WithExportToHost(true),
		NewHeadersAction(
			fmt.Sprintf("has(%s.ok_response)", name),
			HeaderTargetRequest,
			fmt.Sprintf("%s.ok_response.headers", name),
		),
		NewFailAction(
			fmt.Sprintf("!has(%s.denied_response) && !has(%s.ok_response)", name, name),
			fmt.Sprintf("Auth response contained no http_response from %s", name),
		),
	}
}

// --- RateLimit message construction ---

var rateLimitKnownAttrs = [2]string{"ratelimit.domain", "ratelimit.hits_addend"}

func isRateLimitKnownAttr(data DataType) bool {
	var key string
	switch val := data.Value.(type) {
	case *Static:
		key = val.Static.Key
	case *Expression:
		key = val.ExpressionItem.Key
	}
	for _, attr := range rateLimitKnownAttrs {
		if key == attr {
			return true
		}
	}
	return false
}

// ReservationAmountCEL returns the CEL expression this spec carries for
// reservation.amount (RFC 0021), wrapped in uint(), or "" if it carries none.
func (s ActionSpec) ReservationAmountCEL() string {
	if s.Reservation == nil || s.Reservation.Amount == "" {
		return ""
	}
	return fmt.Sprintf("uint(%s)", s.Reservation.Amount)
}

// ReservationTTLCEL returns the CEL expression this spec carries for
// reservation.ttl (RFC 0021), or "" if it carries none.
func (s ActionSpec) ReservationTTLCEL() string {
	if s.Reservation == nil {
		return ""
	}
	return s.Reservation.TTL
}

func findRateLimitKnownAttrCEL(conditionalData []ConditionalData, attrKey string) string {
	for _, cd := range conditionalData {
		for _, item := range cd.Data {
			switch val := item.Value.(type) {
			case *Static:
				if val.Static.Key == attrKey {
					if attrKey == "ratelimit.hits_addend" {
						return fmt.Sprintf("uint(%s)", val.Static.Value)
					}
					return fmt.Sprintf(`"%s"`, escapeCELString(val.Static.Value))
				}
			case *Expression:
				if val.ExpressionItem.Key == attrKey {
					if attrKey == "ratelimit.hits_addend" {
						return fmt.Sprintf("uint(%s)", val.ExpressionItem.Value)
					}
					return val.ExpressionItem.Value
				}
			}
		}
	}
	return ""
}

func buildRateLimitRequest(scope string, conditionalData []ConditionalData, bindings []DataBinding) RateLimitRequestCEL {
	domain := findRateLimitKnownAttrCEL(conditionalData, "ratelimit.domain")
	if domain == "" {
		domain = fmt.Sprintf(`"%s"`, escapeCELString(scope))
	}

	hitsAddend := findRateLimitKnownAttrCEL(conditionalData, "ratelimit.hits_addend")
	if hitsAddend == "" {
		hitsAddend = "1u"
	}

	return RateLimitRequestCEL{
		Domain:      domain,
		HitsAddend:  hitsAddend,
		Descriptors: collectDescriptors(conditionalData, bindings),
	}
}

// collectDescriptors builds the descriptor list shared by the ratelimit, reserve
// and commit requests: conditional-data descriptors (skipping known attrs) plus
// a descriptor derived from bindings.
func collectDescriptors(conditionalData []ConditionalData, bindings []DataBinding) []RateLimitDescriptorCEL {
	var descriptors []RateLimitDescriptorCEL

	if desc := conditionalDataToDescriptor(conditionalData); desc != nil {
		descriptors = append(descriptors, *desc)
	}

	if bindingDesc := bindingsToDescriptor(bindings); bindingDesc != nil {
		descriptors = append(descriptors, *bindingDesc)
	}

	return descriptors
}

func buildReserveRequest(scope string, conditionalData []ConditionalData, bindings []DataBinding, reservation *ReservationSpec) ReserveRequestCEL {
	domain := findRateLimitKnownAttrCEL(conditionalData, "ratelimit.domain")
	if domain == "" {
		domain = fmt.Sprintf(`"%s"`, escapeCELString(scope))
	}

	// amount is required by the message; the controller always emits it (with a
	// repo-defined default when the policy omits it). The fallback here is purely
	// defensive.
	amount := "0u"
	var ttl string
	if reservation != nil {
		if reservation.Amount != "" {
			amount = fmt.Sprintf("uint(%s)", reservation.Amount)
		}
		// ttl is optional; leaving it empty omits the field so Limitador applies
		// its own default.
		ttl = reservation.TTL
	}

	return ReserveRequestCEL{
		Domain:      domain,
		Amount:      amount,
		TTL:         ttl,
		Descriptors: collectDescriptors(conditionalData, bindings),
	}
}

// buildCommitRequest builds the CommitRequest CEL message. hasReservation and
// amountKnown are the same CEL boolean expressions used by buildCommit to
// decide whether to send Commit at all; rawActualAmount is the unwrapped
// reservation.actual_amount expression (e.g. a responseBodyJSON(...) call, or
// its hoisted store-path reference after BuildActions runs).
func buildCommitRequest(scope string, conditionalData []ConditionalData, bindings []DataBinding, reservationStorePath, hasReservation, rawActualAmount, amountKnown string) CommitRequestCEL {
	domain := findRateLimitKnownAttrCEL(conditionalData, "ratelimit.domain")
	if domain == "" {
		domain = fmt.Sprintf(`"%s"`, escapeCELString(scope))
	}

	// Unparseable usage (connection issue, non-JSON/error body) still commits,
	// releasing any held reservation without charging for an unknown amount.
	actualAmount := "0u"
	if rawActualAmount != "" {
		actualAmount = fmt.Sprintf("(%s) ? uint(%s) : 0u", amountKnown, rawActualAmount)
	}

	// No reservation held (e.g. Reserve failed open) but usage is known: commit
	// with an empty reservation_id, which Limitador treats like a plain Report.
	reservationID := fmt.Sprintf(`(%s) ? %s : ""`, hasReservation, reservationStorePath)

	return CommitRequestCEL{
		Domain:        domain,
		ReservationID: reservationID,
		ActualAmount:  actualAmount,
		Descriptors:   collectDescriptors(conditionalData, bindings),
	}
}

func conditionalDataToDescriptor(conditionalData []ConditionalData) *RateLimitDescriptorCEL {
	var unconditionalEntries []DescriptorEntryCEL
	var entryListParts []string

	for _, cd := range conditionalData {
		var entries []DescriptorEntryCEL
		for _, item := range cd.Data {
			if isRateLimitKnownAttr(item) {
				continue
			}
			entries = append(entries, dataTypeToDescriptorEntry(item))
		}
		if len(entries) == 0 {
			continue
		}

		if len(cd.Predicates) == 0 {
			unconditionalEntries = append(unconditionalEntries, entries...)
		} else {
			predicate := joinPredicates(cd.Predicates, "&&")
			part := RateLimitDescriptorCEL{Entries: entries, Predicate: predicate}
			entryListParts = append(entryListParts, part.conditionalEntriesCEL())
		}
	}

	if len(unconditionalEntries) == 0 && len(entryListParts) == 0 {
		return nil
	}

	if len(unconditionalEntries) > 0 {
		strs := make([]string, len(unconditionalEntries))
		for i, e := range unconditionalEntries {
			strs[i] = e.ToCEL()
		}
		entryListParts = append(entryListParts, fmt.Sprintf("[%s]", strings.Join(strs, ", ")))
	}

	if len(entryListParts) == 1 && len(unconditionalEntries) > 0 {
		return &RateLimitDescriptorCEL{Entries: unconditionalEntries}
	}

	return &RateLimitDescriptorCEL{EntriesExpr: strings.Join(entryListParts, " + ")}
}

func dataTypeToDescriptorEntry(data DataType) DescriptorEntryCEL {
	switch val := data.Value.(type) {
	case *Static:
		return DescriptorEntryCEL{
			Key:      val.Static.Key,
			ValueCEL: fmt.Sprintf(`"%s"`, escapeCELString(val.Static.Value)),
		}
	case *Expression:
		return DescriptorEntryCEL{
			Key:      val.ExpressionItem.Key,
			ValueCEL: fmt.Sprintf("string(%s)", val.ExpressionItem.Value),
		}
	default:
		return DescriptorEntryCEL{}
	}
}

func bindingsToDescriptor(bindings []DataBinding) *RateLimitDescriptorCEL {
	if len(bindings) == 0 {
		return nil
	}
	entries := make([]DescriptorEntryCEL, 0, len(bindings))
	for _, b := range bindings {
		key := b.Field
		if b.Domain != "" && b.Domain != "metrics.labels" {
			key = fmt.Sprintf("%s.%s", b.Domain, b.Field)
		}
		entries = append(entries, DescriptorEntryCEL{
			Key:      key,
			ValueCEL: fmt.Sprintf("string(%s)", b.Expression),
		})
	}
	return &RateLimitDescriptorCEL{Entries: entries}
}

// --- RateLimit on_reply ---

// tokenRateLimitDenyBody is an OpenAI-style JSON error, so OpenAI-compatible
// clients (which expect a JSON error body, not plain text) can parse a
// TokenRateLimitPolicy denial instead of failing to decode it.
const tokenRateLimitDenyBody = `"{\"error\": {\"message\": \"Too Many Requests\", \"type\": \"rate_limit_exceeded\", \"code\": 429}}"` //nolint:gosec

// tokenRateLimitDenyContentTypeHeader pairs with tokenRateLimitDenyBody: without
// an explicit content-type header, the DenyResponse defaults to none, and the
// data plane serves the body as text/plain regardless of its actual content -
// OpenAI-compatible clients then refuse to parse it as JSON.
const tokenRateLimitDenyContentTypeHeader = `["content-type", "application/json"]` //nolint:gosec

func buildRateLimitOnReply(name string, tokenBased bool) []Action {
	denyBody := `"Too Many Requests\n"`
	denyHeaders := fmt.Sprintf("%s.response_headers_to_add", name)
	if tokenBased {
		denyBody = tokenRateLimitDenyBody
		denyHeaders = fmt.Sprintf("%s.response_headers_to_add + [%s]", name, tokenRateLimitDenyContentTypeHeader)
	}
	return []Action{
		NewDenyAction(
			fmt.Sprintf("%s.overall_code == 2", name),
			fmt.Sprintf(
				`DenyResponse{status: 429u, headers: %s, body: %s}`,
				denyHeaders, denyBody,
			),
		),
		NewHeadersAction(
			fmt.Sprintf("%s.overall_code == 1", name),
			HeaderTargetResponse,
			fmt.Sprintf("%s.response_headers_to_add", name),
		),
		NewFailAction(
			fmt.Sprintf("%s.overall_code != 1 && %s.overall_code != 2", name, name),
			fmt.Sprintf("Unknown rate limit response code from %s", name),
		),
	}
}

func buildReportOnReply(name string) []Action {
	return []Action{
		NewFailAction(
			fmt.Sprintf("!has(%s.overall_code)", name),
			"Rate limit report failed: invalid gRPC response",
		).WithTerminal(false).WithGuard(false),
	}
}

// --- Reserve/Commit on_reply ---

// buildReserveOnReply handles the ReserveResponse in the request (guard) phase:
//   - code OVER_LIMIT (2): deny the request with 429.
//   - code OK (1) with a reservation_id: stash the id at the per-limit store path
//     so the paired Commit can read it back. A missing reservation_id (e.g.
//     failed-open) leaves the path unset; Commit still fires (RFC 0021), just
//     with an empty reservation_id, degrading to Report-style accounting.
//   - any other code: fail (invalid/unknown response).
func buildReserveOnReply(name, id string) []Action {
	return []Action{
		NewDenyAction(
			fmt.Sprintf("%s.code == 2", name),
			fmt.Sprintf(`DenyResponse{status: 429u, headers: [%s], body: %s}`, tokenRateLimitDenyContentTypeHeader, tokenRateLimitDenyBody),
		),
		NewStoreAction(
			fmt.Sprintf("%s.code == 1 && has(%s.reservation_id)", name, name),
			reservationStorePath(id),
			fmt.Sprintf("%s.reservation_id", name),
		),
		NewFailAction(
			fmt.Sprintf("%s.code != 1 && %s.code != 2", name, name),
			fmt.Sprintf("Unknown reserve response code from %s", name),
		),
	}
}

// buildCommitOnReply handles the CommitResponse in the response phase. There is
// nothing to enforce on a commit; a malformed response is a non-terminal failure.
func buildCommitOnReply(name string) []Action {
	return []Action{
		NewFailAction(
			fmt.Sprintf("!has(%s.reservation_released)", name),
			"Reserve commit failed: invalid gRPC response",
		).WithTerminal(false).WithGuard(false),
	}
}
