package common

func AddAnnotation(annotation string, value string, annotations map[string]string) map[string]string {
	_, ok := annotations[annotation]
	if !ok && len(value) == 0 {
		return annotations
	}

	if len(value) == 0 {
		delete(annotations, annotation)
		return annotations
	}

	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[annotation] = value
	return annotations
}

func AnnotationEqual(annotation string, value string, annotations map[string]string) bool {
	val, ok := annotations[annotation]
	if !ok && len(annotation) == 0 {
		return true
	}

	if val == value {
		return true
	}

	return false
}
