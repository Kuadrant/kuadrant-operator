package rlptools

import (
	"fmt"

	"github.com/kuadrant/kuadrant-controller/pkg/common"
)

var (
	LimitadorNamespace = common.FetchEnv("LIMITADOR_NAMESPACE", common.KuadrantNamespace)
	LimitadorName      = common.FetchEnv("LIMITADOR_NAME", "limitador")
)

func LimitadorServiceClusterHost(svcName string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", svcName, LimitadorNamespace)
}
