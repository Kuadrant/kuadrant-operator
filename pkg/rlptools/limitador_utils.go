package rlptools

import (
	"github.com/kuadrant/kuadrant-controller/pkg/common"
)

//TODO: we might want to have every single ENV in the same file
var (
	LimitadorNamespace = common.FetchEnv("LIMITADOR_NAMESPACE", common.KuadrantNamespace)
	LimitadorName      = common.FetchEnv("LIMITADOR_NAME", "limitador")
)
