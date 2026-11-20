//go:build windows

package printer

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"
)

var spoolDLL = syscall.NewLazyDLL("winspool.drv")

var openPrinter = spoolDLL.NewProc("OpenPrinterW")

var closePrinter = spoolDLL.NewProc("ClosePrinter")

var startDocPrinter = spoolDLL.NewProc("StartDocPrinterW")

var endDocPrinter = spoolDLL.NewProc("EndDocPrinter")

var writePrinter = spoolDLL.NewProc("WritePrinter")

var abortPrinter = spoolDLL.NewProc("AbortPrinter")

func usbQueues(ctx context.Context) ([]string, error) {
	// Fixed script; printer names are never interpolated into executable code.
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; ConvertTo-Json -Compress -InputObject @((Get-Printer -ErrorAction Stop).Name)`)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sistemski printeri nisu dostupni: %w", err)
	}
	var names []string
	err = json.Unmarshal(data, &names)
	return names, err
}

func usbSend(req localRequest) (string, string) {
	name, err := syscall.UTF16PtrFromString(req.Queue)
	if err != nil {
		return "failed", err.Error()
	}
	var handle uintptr
	ok, _, err := openPrinter.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&handle)), 0)
	if ok == 0 {
		return "failed", "USB red nije otvoren: " + err.Error()
	}
	defer closePrinter.Call(handle)
	if req.Probe {
		return "sent", "Sistemski red je otvoren. USB veza i stanje papira nisu potvrđeni."
	}
	title, _ := syscall.UTF16PtrFromString("Receipt Studio")
	raw, _ := syscall.UTF16PtrFromString("RAW")
	info := struct{ Name, Output, Datatype *uint16 }{title, nil, raw}
	id, _, err := startDocPrinter.Call(handle, 1, uintptr(unsafe.Pointer(&info)))
	if id == 0 {
		return "failed", "RAW posao nije otvoren: " + err.Error()
	}
	complete := false
	defer func() {
		if !complete {
			abortPrinter.Call(handle)
		}
	}()
	written := 0
	for written < len(req.Bytes) {
		var n uint32
		ok, _, _ := writePrinter.Call(handle, uintptr(unsafe.Pointer(&req.Bytes[written])), uintptr(len(req.Bytes)-written), uintptr(unsafe.Pointer(&n)))
		written += int(n)
		if ok == 0 || n == 0 {
			return "unknown", "RAW slanje je prekinuto. Provjeri sistemski red prije ponavljanja."
		}
	}
	ok, _, _ = endDocPrinter.Call(handle)
	if ok == 0 {
		return "unknown", "Sistemski red nije potvrdio završetak. Provjeri printer."
	}
	complete = true
	return "sent", fmt.Sprintf("Predano sistemskom redu (RAW), posao %d. Fizičko printanje nije potvrđeno.", id)
}
