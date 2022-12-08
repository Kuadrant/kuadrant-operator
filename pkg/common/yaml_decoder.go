package common

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/yaml"
)

type DecodeCallback = func(runtime.Object) error

func DecodeFile(ctx context.Context, fileData []byte, scheme *runtime.Scheme, cb DecodeCallback) error {
	logger, _ := logr.FromContext(ctx)
	codec := serializer.NewCodecFactory(scheme)
	decoder := codec.UniversalDeserializer()

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

		if n == 0 {
			// empty docs
			continue
		}

		docData := buf[:n]
		obj, _, err := decoder.Decode(docData, nil, nil)
		if err != nil {
			logger.Info("Document decode error", "error", err)
			continue
		}

		err = cb(obj)
		if err != nil {
			return err
		}
	}
	return nil
}
