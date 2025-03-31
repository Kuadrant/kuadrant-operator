package kuadrant

import (
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

func stringOrError(str string, err error) ref.Val {
	if err != nil {
		return types.NewErr(err.Error())
	}
	return types.String(str)
}
