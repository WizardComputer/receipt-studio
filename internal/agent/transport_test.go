package agent

import (
	"context"
	"testing"

	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/printer"
)

func TestLocalPrinterSnapshotAndReprint(t *testing.T) {
	for _, connection := range []model.Connection{
		{Kind: "usb", Queue: "EPSON TM-T20IV"},
	} {
		t.Run(connection.Kind, func(t *testing.T) {
			a := testAgent(t)
			p := model.Printer{ID: "local", Name: "Local printer", Connection: connection, TemplateID: a.config.Templates[0].ID}
			if _, err := a.SavePrinter(p, true); err != nil {
				t.Fatal(err)
			}
			j, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "local-job", Data: sampleFor(a, p)})
			if err != nil {
				t.Fatal(err)
			}
			if j.Connection != connection {
				t.Fatal("connection not captured")
			}
			editTestJob(t, a, j.ID, func(j *model.Job) { j.Status = "sent" })
			a.config.Printers[0].Connection = model.Connection{Kind: "usb", Queue: "Another printer"}
			summary, err := a.ReprintJob("local-job")
			if err != nil {
				t.Fatal(err)
			}
			id := summary["id"].(string)
			if mustJob(t, a, id).Connection != connection {
				t.Fatal("reprint used new settings")
			}
			detail, err := a.JobDetails("local-job")
			if err != nil {
				t.Fatal(err)
			}
			if detail["printer"].(model.Printer).Connection != connection {
				t.Fatal("details lost original settings")
			}
			loaded, err := openTestAgent(t, a.dir)
			if err != nil {
				t.Fatal(err)
			}
			if mustJob(t, loaded, id).Connection != connection {
				t.Fatal("disk lost snapshot")
			}
		})
	}
}

func TestSerialConnectionRejected(t *testing.T) {
	a := testAgent(t)
	p := model.Printer{ID: "serial", Name: "Serial", Connection: model.Connection{Kind: "serial"}, TemplateID: a.config.Templates[0].ID}
	_, err := a.SavePrinter(p, true)
	requireActionFailure(t, err, model.InvalidInput)
	_, err = printer.ProbePrinter(context.Background(), p)
	requireActionFailure(t, err, model.InvalidInput)
}
