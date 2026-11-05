package version

// version is injected once at build time and shared by the operator and its generated resources.
var version = "unknown"

// String returns the version embedded in this build.
func String() string {
	return version
}
