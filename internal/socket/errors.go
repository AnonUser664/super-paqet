// File errors.go: names decoder outcomes that mean ignore this captured frame rather than
// close the carrier.

package socket

import "errors"

// errNoPayload marks ignorable captured frames; it is not a permanent failure of the carrier.
var errNoPayload = errors.New("socket: packet carries no payload")
