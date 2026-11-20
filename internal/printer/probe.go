package printer

import (
	"context"
	"encoding/json"
	"net"
	"time"

	"receiptstudio.local/agent/internal/model"
)

func ProbePrinter(ctx context.Context, p model.Printer) (string, error) {
	if err := p.ValidateConnection(); err != nil {
		return "", model.ActionFailure(model.InvalidInput, err.Error())
	}
	if p.TransportKind() != "tcp" {
		status, message := sendLocal(ctx, p.Connection, nil, true)
		if status != "sent" {
			return "", model.ActionFailure(model.ConnectionFailure, message)
		}
		return message, nil
	}
	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(p.Host, fmtPort(p.Port)))
	if err != nil {
		return "", model.ActionFailure(model.ConnectionFailure, "Printer ne odgovara na podešenoj IP adresi i portu.")
	}
	_ = conn.Close()
	return "TCP veza je dostupna. Stanje papira nije provjereno.", nil
}

func fmtPort(port int) string { b, _ := json.Marshal(port); return string(b) }
