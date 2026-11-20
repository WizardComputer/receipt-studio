package printer

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"time"

	"receiptstudio.local/agent/internal/model"
)

type localRequest struct {
	model.Connection
	Bytes []byte `json:"bytes"`
	Probe bool   `json:"probe"`
}

type localResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Isolate blocking drivers in a child process. On timeout the child is killed;
// no background writer survives to print after the job has been marked unknown.
func sendLocal(ctx context.Context, c model.Connection, data []byte, probe bool) (string, string) {
	timeout := 30 * time.Second
	if probe {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		return "failed", err.Error()
	}
	input, _ := json.Marshal(localRequest{Connection: c, Bytes: data, Probe: probe})
	cmd := exec.CommandContext(ctx, executable, "--local-printer-worker")
	configureLocalCommand(cmd)
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.Output()
	if err != nil {
		if probe {
			return "failed", "Uređaj nije dostupan ili provjera nije završena na vrijeme."
		}
		return "unknown", "Slanje lokalnom printeru nije potvrđeno. Provjeri printer i sistemski red prije ponavljanja."
	}
	var result localResult
	if json.Unmarshal(output, &result) != nil {
		return "unknown", "Neispravan odgovor lokalnog transporta. Provjeri printer."
	}
	return result.Status, result.Message
}

func RunLocalPrinterWorker() {
	var req localRequest
	if err := json.NewDecoder(io.LimitReader(os.Stdin, 16<<20)).Decode(&req); err != nil {
		os.Exit(2)
	}
	p := model.Printer{Connection: req.Connection}
	if err := p.ValidateConnection(); err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(localResult{"failed", err.Error()})
		return
	}
	var status, message string
	switch req.TransportKind() {
	case "usb":
		status, message = usbSend(req)
	default:
		status, message = "failed", "Nepodržan lokalni transport."
	}
	_ = json.NewEncoder(os.Stdout).Encode(localResult{status, message})
}

type LocalDevices struct {
	USB      []string `json:"usb"`
	Warnings []string `json:"warnings"`
}

func DiscoverLocalDevices(parent context.Context) LocalDevices {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	queues, queueErr := usbQueues(ctx)
	if queues == nil {
		queues = []string{}
	}
	warnings := []string{}
	if queueErr != nil {
		warnings = append(warnings, "USB redovi: "+queueErr.Error())
	}
	return LocalDevices{USB: queues, Warnings: warnings}
}
