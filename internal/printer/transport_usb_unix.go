//go:build !windows

package printer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func cupsCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "/usr/bin/"+name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "CUPS_SERVER=localhost")
	return cmd
}

func usbQueues(ctx context.Context) ([]string, error) {
	output, err := cupsCommand(ctx, "lpstat", "-e").Output()
	if err != nil {
		return nil, fmt.Errorf("sistemski printeri nisu dostupni: %w", err)
	}
	return strings.Fields(string(output)), nil
}

func usbSend(req localRequest) (string, string) {
	queues, err := usbQueues(context.Background())
	if err != nil {
		return "failed", err.Error()
	}
	found := false
	for _, q := range queues {
		if q == req.Queue {
			found = true
		}
	}
	if !found {
		return "failed", "Sistemski red nije pronađen. Dodaj USB printer u postavkama operativnog sistema."
	}
	if req.Probe {
		return "sent", "Sistemski red postoji. USB veza, RAW podrška i stanje papira nisu potvrđeni."
	}
	cmd := cupsCommand(context.Background(), "lp", "-d", req.Queue, "-o", "raw", "-o", "job-sheets=none", "-t", "Receipt Studio", "--", "-")
	cmd.Stdin = bytes.NewReader(req.Bytes)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "unknown", "Sistemski red nije potvrdio prihvat: " + strings.TrimSpace(string(output)) + " Provjeri red prije ponavljanja."
	}
	return "sent", "Predano sistemskom redu (RAW): " + strings.TrimSpace(string(output)) + ". Fizičko printanje nije potvrđeno."
}
