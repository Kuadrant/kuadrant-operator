package protocol

import (
	"fmt"

	"github.com/Masterminds/semver/v3"
)

// Version is the extension protocol version negotiated during handshake.
// Bump the minor version for any behavioral/semantic change to the
// handshake contract.
const Version = "0.1.0"

func Compatible(a, b string) error {
	left, err := semver.StrictNewVersion(a)
	if err != nil {
		return fmt.Errorf("invalid protocol version %q: %w", a, err)
	}
	right, err := semver.StrictNewVersion(b)
	if err != nil {
		return fmt.Errorf("invalid protocol version %q: %w", b, err)
	}
	if left.Major() != right.Major() || (left.Major() == 0 && left.Minor() != right.Minor()) {
		return fmt.Errorf("protocol version %s is not compatible with %s", a, b)
	}
	return nil
}
