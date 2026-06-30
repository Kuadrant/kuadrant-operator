package wasm

func NewDenyAction(predicate, denyWith string) *DenyAction {
	return &DenyAction{
		ActionBase: ActionBase{Predicate: predicate, Terminal: true, IsGuard: true},
		DenyWith:   denyWith,
	}
}

func NewHeadersAction(predicate, target, headers string) *HeadersAction {
	return &HeadersAction{
		ActionBase: ActionBase{Predicate: predicate, Terminal: false, IsGuard: true},
		Target:     target,
		Headers:    headers,
	}
}

func NewStoreAction(predicate, path, value string) *StoreAction {
	return &StoreAction{
		ActionBase: ActionBase{Predicate: predicate, Terminal: false, IsGuard: true},
		Path:       path,
		Value:      value,
	}
}

func NewFailAction(predicate, logMessage string) *FailAction {
	return &FailAction{
		ActionBase: ActionBase{Predicate: predicate, Terminal: true, IsGuard: true},
		LogMessage: logMessage,
	}
}

func NewGrpcAction(predicate, varName, service, messageBuilder, label string) *GrpcAction {
	return &GrpcAction{
		ActionBase:     ActionBase{Predicate: predicate, Terminal: false, IsGuard: true},
		Var:            varName,
		Service:        service,
		MessageBuilder: messageBuilder,
		Label:          label,
	}
}

// With* methods on GrpcAction

func (a *GrpcAction) WithTerminal(terminal bool) *GrpcAction {
	a.Terminal = terminal
	return a
}

func (a *GrpcAction) WithGuard(isGuard bool) *GrpcAction {
	a.IsGuard = isGuard
	return a
}

func (a *GrpcAction) WithSources(sources []string) *GrpcAction {
	a.SourcePolicyLocators = sources
	return a
}

func (a *GrpcAction) WithOnReply(onReply ...TypedAction) *GrpcAction {
	a.OnReply = onReply
	return a
}

// With* methods on DenyAction

func (a *DenyAction) WithTerminal(terminal bool) *DenyAction {
	a.Terminal = terminal
	return a
}

func (a *DenyAction) WithGuard(isGuard bool) *DenyAction {
	a.IsGuard = isGuard
	return a
}

func (a *DenyAction) WithSources(sources []string) *DenyAction {
	a.SourcePolicyLocators = sources
	return a
}

// With* methods on HeadersAction

func (a *HeadersAction) WithTerminal(terminal bool) *HeadersAction {
	a.Terminal = terminal
	return a
}

func (a *HeadersAction) WithGuard(isGuard bool) *HeadersAction {
	a.IsGuard = isGuard
	return a
}

func (a *HeadersAction) WithSources(sources []string) *HeadersAction {
	a.SourcePolicyLocators = sources
	return a
}

// With* methods on StoreAction

func (a *StoreAction) WithTerminal(terminal bool) *StoreAction {
	a.Terminal = terminal
	return a
}

func (a *StoreAction) WithGuard(isGuard bool) *StoreAction {
	a.IsGuard = isGuard
	return a
}

func (a *StoreAction) WithSources(sources []string) *StoreAction {
	a.SourcePolicyLocators = sources
	return a
}

func (a *StoreAction) WithExportToHost(exportToHost bool) *StoreAction {
	a.ExportToHost = exportToHost
	return a
}

// With* methods on FailAction

func (a *FailAction) WithTerminal(terminal bool) *FailAction {
	a.Terminal = terminal
	return a
}

func (a *FailAction) WithGuard(isGuard bool) *FailAction {
	a.IsGuard = isGuard
	return a
}

func (a *FailAction) WithSources(sources []string) *FailAction {
	a.SourcePolicyLocators = sources
	return a
}
