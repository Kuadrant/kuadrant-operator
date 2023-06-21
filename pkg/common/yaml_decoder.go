package common

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/yaml"
)

type DecodeCallback = func(runtime.Object) error

// DecodeFile decodes the provided file data (encoded YAML documents) into Kubernetes objects using the specified scheme,
// and invokes the callback function for each decoded object. Returns an error if any decoding error occurs.
func DecodeFile(ctx context.Context, fileData []byte, scheme *runtime.Scheme, cb DecodeCallback) error {
	logger, logErr := logr.FromContext(ctx)
	codec := serializer.NewCodecFactory(scheme)
	decoder := codec.UniversalDeserializer()

	if logErr != nil {
		return logErr
	}

	// the maximum size used to buffer a doc 5M
	buf := make([]byte, 5*1024*1024)
	docDecoder := yaml.NewDocumentDecoder(io.NopCloser(bytes.NewReader(fileData)))

	for {
		n, err := docDecoder.Read(buf)
		if err != nil {
			if errors.Is(io.EOF, err) {
				break
			}
			return err
		}

		if n == 0 || string(fileData) == "---" {
			// Skip empty docs
			continue
		}

		docData := buf[:n]
		obj, _, err := decoder.Decode(docData, nil, nil)
		if err != nil {
			logger.Info("Document decode error", "error", err)
			return fmt.Errorf("failed to decode document: %w", err)
		}

		err = cb(obj)
		if err != nil {
			return err
		}
	}
	return nil
}
