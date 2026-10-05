// File tcp.go: encodes configured outer TCP flag cycles; these flags do not create an ordinary
// TCP connection.

package conf

import (
	"fmt"
)

// TCP keeps configured string cycles and their parsed outer flag combinations.
type TCP struct {
	// Configured local outer flag cycle strings before parsing.
	LF_ []string `yaml:"local_flag"`
	// Configured requested peer outer flag cycle strings before parsing.
	RF_ []string `yaml:"remote_flag"`
	// Parsed local flag cycle used by the packet encoder.
	LF []TCPF `yaml:"-"`
	// Parsed peer flag cycle sent through reliable inner setup.
	RF []TCPF `yaml:"-"`
}

// TCPF represents the individual fabricated outer TCP flag bits independently of a kernel TCP
// connection.
type TCPF struct {
	// Individual outer TCP flag choices packed into peer setup; KCP remains responsible for
	// reliability.
	FIN, SYN, RST, PSH, ACK, URG, ECE, CWR, NS bool
}

// setDefaults retains PA flag cycles when none were provided so omitted knobs preserve the
// baseline envelope.
func (t *TCP) setDefaults() {
	if len(t.LF_) == 0 {
		t.LF_ = []string{"PA"}
	}
	if len(t.RF_) == 0 {
		t.RF_ = []string{"PA"}
	}
}

// validate parses bounded flag cycles once so hot packet writers use validated flag objects.
func (t *TCP) validate() []error {
	var errors []error

	if len(t.LF_) != 0 {
		t.LF = make([]TCPF, len(t.LF_))
		for i, fStr := range t.LF_ {
			f, err := strTCPF(fStr)
			if err != nil {
				errors = append(errors, err)
			}
			t.LF[i] = f
		}
	}
	if len(t.RF_) != 0 {
		t.RF = make([]TCPF, len(t.RF_))
		for i, fStr := range t.RF_ {
			f, err := strTCPF(fStr)
			if err != nil {
				errors = append(errors, err)
			}
			t.RF[i] = f
		}
	}

	if len(t.LF) == 0 || len(t.RF) == 0 {
		errors = append(errors, fmt.Errorf("at least one TCP flag combination required"))
	}

	maxTCPFLen := 64
	if len(t.LF_) > maxTCPFLen {
		errors = append(errors, fmt.Errorf("local_flag exceeds max %d", maxTCPFLen))
	}
	if len(t.RF_) > maxTCPFLen {
		errors = append(errors, fmt.Errorf("remote_flag exceeds max %d", maxTCPFLen))
	}

	return errors
}

// strTCPF parses uppercase flag combinations so the wire encoder never has to interpret
// configuration strings.
func strTCPF(fStr string) (TCPF, error) {
	var f TCPF
	for _, ch := range fStr {
		switch ch {
		case 'F':
			f.FIN = true
		case 'S':
			f.SYN = true
		case 'R':
			f.RST = true
		case 'P':
			f.PSH = true
		case 'A':
			f.ACK = true
		case 'U':
			f.URG = true
		case 'E':
			f.ECE = true
		case 'C':
			f.CWR = true
		case 'N':
			f.NS = true
		default:
			return f, fmt.Errorf("invalid TCP flag '%c' in combination", ch)
		}
	}
	return f, nil
}
