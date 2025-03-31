package kuadrant

import (
	"math"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

func CelExt() cel.EnvOption {
	l := &kuadrantLib{
		version: math.MaxUint32,
		dev:     false,
	}
	return cel.Lib(l)
}

type kuadrantLib struct {
	version uint32
	dev     bool
}

func (l kuadrantLib) CompileOptions() []cel.EnvOption {
	opts := []cel.EnvOption{}

	constVersion := "0"

	// eventually add functions for v1 here:
	if l.version >= 1 {
		constVersion = "1"
	}

	// only dev adds anything for now really
	if l.dev {
		constVersion = constVersion + "_dev"
	}

	opts = append(opts,
		cel.Constant("__KUADRANT_VERSION", cel.StringType, types.String(constVersion)),
	)

	return opts
}

func (l kuadrantLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{}
}

// LibraryName implements the SingletonLibrary interface method.
func (kuadrantLib) LibraryName() string {
	return "kuadrant.cel.ext.kuadrant"
}
