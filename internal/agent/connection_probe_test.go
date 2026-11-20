package agent

import (
	"context"
	"net"
	"testing"

	"receiptstudio.local/agent/internal/model"
)

func TestDraftConnectionDoesNotSaveOrPrint(t *testing.T) {
	a := testAgent(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan int, 1)
	go func() {
		c, err := listener.Accept()
		if err != nil {
			done <- -1
			return
		}
		defer c.Close()
		buf := make([]byte, 1)
		n, _ := c.Read(buf)
		done <- n
	}()
	p := model.Printer{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port}
	if r := request(a, "POST", "/api/printer-connection", p, a.config.APIKey, ""); r.Code != 404 {
		t.Fatal("public key accessed admin probe")
	}
	_, err = a.ProbePrinter(context.Background(), model.Printer{Host: "8.8.8.8", Port: 9100})
	requireActionFailure(t, err, model.InvalidInput)
	if _, err := a.ProbePrinter(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if n := <-done; n != 0 {
		t.Fatal("probe sent data", n)
	}
	page, err := a.store.ListDiagnostics(model.DiagnosticQuery{Component: "printer"})
	if err != nil || len(page.Items) != 2 || page.Items[0].Level != "info" || page.Items[1].Level != "warning" {
		t.Fatal(page, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.ProbePrinter(ctx, p); err == nil {
		t.Fatal("cancelled probe succeeded")
	}
	page, err = a.store.ListDiagnostics(model.DiagnosticQuery{Component: "printer", Level: "error"})
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	if countTestJobs(t, a) != 0 || len(a.config.Printers) != 0 {
		t.Fatal("probe changed configuration or history")
	}
}
