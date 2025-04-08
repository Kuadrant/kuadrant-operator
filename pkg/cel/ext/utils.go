package kuadrant

import (
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func refToProto[T protoreflect.ProtoMessage](val ref.Val) (t T, err error) {
	value, err := cel.RefValueToValue(val)
	if err != nil {
		return t, err
	}
	v, err := value.GetObjectValue().UnmarshalNew()
	if err != nil {
		return t, err
	}
	return v.(T), nil
}
