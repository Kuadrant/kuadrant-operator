package consoleplugin

// Runtime describes the serving configuration required by a plugin image.
// It is selected with the image stream, including when an image is mirrored
// or referenced by digest.
type Runtime string

const (
	RuntimeNginx Runtime = "nginx"
	RuntimeGo    Runtime = "go"
)

type Image struct {
	URL     string
	Runtime Runtime
}
