package printer

import (
	"testing"

	"receiptstudio.local/agent/internal/model"
)

func TestConnectionValidation(t *testing.T) {
	for _, p := range []model.Printer{
		{Host: "192.168.0.25", Port: 9100},
		{Connection: model.Connection{Kind: "usb", Queue: "EPSON TM-T20IV"}},
	} {
		if err := p.ValidateConnection(); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []model.Connection{
		{Kind: "serial"}, {Kind: "bluetooth"}, {Kind: "usb"}, {Kind: "usb", Queue: "bad\nqueue"},
	} {
		p := model.Printer{Connection: c}
		if p.ValidateConnection() == nil {
			t.Fatalf("accepted invalid connection: %+v", c)
		}
	}
}
