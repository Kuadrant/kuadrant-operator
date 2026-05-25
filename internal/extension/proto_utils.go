package extension

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/types/descriptorpb"
)

func findResponseMessageType(fds *descriptorpb.FileDescriptorSet, serviceName, methodName string) string {
	for _, file := range fds.File {
		for _, svc := range file.Service {
			fullServiceName := svc.GetName()
			if file.Package != nil && *file.Package != "" {
				fullServiceName = *file.Package + "." + svc.GetName()
			}
			if fullServiceName != serviceName {
				continue
			}
			for _, method := range svc.Method {
				if method.GetName() == methodName {
					return method.GetOutputType()
				}
			}
		}
	}
	return ""
}

func findMessageDescriptor(fds *descriptorpb.FileDescriptorSet, messageType string) *descriptorpb.DescriptorProto {
	normalizedType := strings.TrimPrefix(messageType, ".")
	for _, file := range fds.File {
		pkg := file.GetPackage()
		for _, msg := range file.MessageType {
			fqn := msg.GetName()
			if pkg != "" {
				fqn = pkg + "." + msg.GetName()
			}
			if fqn == normalizedType {
				return msg
			}
		}
	}
	return nil
}

func validateFieldAccess(fds *descriptorpb.FileDescriptorSet, messageType string, fieldPath []string) error {
	if len(fieldPath) == 0 {
		return nil
	}

	msg := findMessageDescriptor(fds, messageType)
	if msg == nil {
		return fmt.Errorf("message type %q not found in proto descriptors", messageType)
	}

	currentMsg := msg
	for i, fieldName := range fieldPath {
		var found *descriptorpb.FieldDescriptorProto
		for _, f := range currentMsg.Field {
			if f.GetName() == fieldName {
				found = f
				break
			}
		}
		if found == nil {
			return fmt.Errorf("field %q not found on message %q", fieldName, currentMsg.GetName())
		}

		if i < len(fieldPath)-1 {
			if found.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
				return fmt.Errorf("field %q on message %q is not a message type, cannot access sub-fields", fieldName, currentMsg.GetName())
			}
			nextMsg := findMessageDescriptor(fds, found.GetTypeName())
			if nextMsg == nil {
				return fmt.Errorf("message type %q for field %q not found", found.GetTypeName(), fieldName)
			}
			currentMsg = nextMsg
		}
	}
	return nil
}
