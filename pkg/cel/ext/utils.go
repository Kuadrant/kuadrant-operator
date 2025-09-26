package ext

import (
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// refToProto converts a CEL ref.Val representing a protobuf object (backed by
// a cel.Value with ObjectValue set) into a concrete protobuf message of type T.
//
// It returns an error if:
//   - The ref.Val cannot be converted to a cel.Value (unexpected type adapter)
//   - The underlying object cannot be unmarshaled into a new instance.
//
// The generic type parameter T must be a protobuf message interface generated
// by protoc. Callers usually specify it via inference: refToProto[*v1.Policy](v).
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
