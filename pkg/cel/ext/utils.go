package kuadrant

import (
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func stringOrError(str string, err error) ref.Val {
	if err != nil {
		return types.NewErr(err.Error())
	}
	return types.String(str)
}

func refToProto[T protoreflect.ProtoMessage](val ref.Val) (T, error) {
	var t T
	if pb, err := cel.RefValueToValue(val); err != nil {
		return t, err
	} else {
		if v, err := pb.GetObjectValue().UnmarshalNew(); err != nil {
			return t, err
		} else {
			return v.(T), nil
		}
	}
}
