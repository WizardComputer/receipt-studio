package model

import (
	"errors"
	"regexp"
)

// Connection is copied into each job: reprinting never uses edited settings.
// LAN TCP is the default connection when Kind is omitted.
type Connection struct {
	Kind  string `json:"connection,omitempty"`
	Queue string `json:"queue,omitempty"`
}

func (c Connection) TransportKind() string {
	if c.Kind == "" {
		return "tcp"
	}
	return c.Kind
}

func (p *Printer) ValidateConnection() error {
	switch p.TransportKind() {
	case "tcp":
		return ValidateAddress(p.Host, p.Port)
	case "usb":
		if len(p.Queue) == 0 || len(p.Queue) > 200 || regexp.MustCompile(`[\x00-\x1f]`).MatchString(p.Queue) {
			return errors.New("Odaberi sistemski red USB printera.")
		}
	default:
		return errors.New("Nepoznat način povezivanja printera.")
	}
	return nil
}
