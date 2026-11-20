package printer

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"receiptstudio.local/agent/internal/model"
)

func Transmit(ctx context.Context, job model.Job) (string, string) {
	if job.TransportKind() != "tcp" {
		return sendLocal(ctx, job.Connection, job.Bytes, false)
	}
	dialer := net.Dialer{Timeout: 4 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(job.Host, strconv.Itoa(job.Port)))
	if err != nil {
		return "failed", "Veza s printerom nije uspostavljena. Nijedan bajt nije poslan."
	}
	defer conn.Close()
	if err := conn.SetWriteDeadline(time.Now().Add(8 * time.Second)); err != nil {
		return "failed", "Nije moguće pripremiti vezu za slanje."
	}
	written := 0
	for written < len(job.Bytes) {
		n, err := conn.Write(job.Bytes[written:])
		written += n
		if err != nil || n == 0 {
			if written == 0 {
				return "failed", "Slanje nije započelo. Provjeri printer i ponovi printanje."
			}
			return "unknown", "Veza je prekinuta tokom slanja. Provjeri račun na printeru prije novog printanja."
		}
	}
	if err := conn.Close(); err != nil {
		return "unknown", "Bajtovi su poslani, ali veza nije uredno zatvorena. Provjeri printer."
	}
	return "sent", fmt.Sprintf("Poslano %d bajtova. Fizičko printanje nije potvrđeno.", written)
}
