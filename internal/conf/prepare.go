package conf

import (
	"fmt"
	"math"
	"strings"
	"time"
)

func writeErr(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	messages := make([]string, len(errs))
	for i, err := range errs {
		messages[i] = err.Error()
	}
	return fmt.Errorf("validation failed:\n  - %s", strings.Join(messages, "\n  - "))
}

// PrepareNetwork resolves and validates a network after automatic discovery.
func PrepareNetwork(n *Network, role string) error {
	n.setDefaults(role)
	return writeErr(n.validate())
}

// PrepareKCP applies the existing encryption derivation and transport defaults.
func PrepareKCP(k *KCP, role string) error {
	k.setDefaults(role)
	if k.Smuxkalive_ < 1 || int64(k.Smuxkalive_) > math.MaxInt64/int64(time.Second) ||
		k.Smuxktimeout_ < 1 || int64(k.Smuxktimeout_) > math.MaxInt64/int64(time.Second) {
		return fmt.Errorf("keepalive seconds exceed duration bounds")
	}
	if errs := k.validate(); len(errs) > 0 {
		return writeErr(errs)
	}
	if k.Smuxkalive <= 0 || k.Smuxktimeout <= k.Smuxkalive {
		return fmt.Errorf("keepalive timeout must exceed a positive interval")
	}
	if k.Streambuf > k.Smuxbuf {
		return fmt.Errorf("stream buffer cannot exceed session receive budget")
	}
	if k.Smuxbuf > 2147483647 || k.Streambuf > 2147483647 {
		return fmt.Errorf("smux buffer ceilings must fit a signed 32-bit window")
	}
	overhead := 20
	if k.Block == nil {
		overhead = 0
	}
	if aead, ok := k.Block.(interface {
		NonceSize() int
		Overhead() int
	}); ok {
		overhead = aead.NonceSize() + aead.Overhead()
	}
	if k.Dshard > 0 {
		overhead += 8
	}
	if k.MTU < overhead+50 {
		return fmt.Errorf("KCP MTU is too small for encryption/FEC overhead")
	}
	if (k.Dshard == 0) != (k.Pshard == 0) || k.Dshard < 0 || k.Pshard < 0 ||
		k.Dshard > 255 || k.Pshard > 255 || k.Dshard+k.Pshard > 256 {
		return fmt.Errorf("FEC requires positive data/parity shards with total <=256, or both zero")
	}
	if k.Mode == "manual" && (k.Interval < 10 || k.Interval > 5000 || k.NoDelay < 0 || k.NoDelay > 1 || k.Resend < 0 || k.Resend > 2 || k.NoCongestion < 0 || k.NoCongestion > 1) {
		return fmt.Errorf("invalid manual KCP tuning")
	}
	return nil
}
