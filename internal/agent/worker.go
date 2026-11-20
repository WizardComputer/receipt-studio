package agent

import (
	"context"
	"time"

	"receiptstudio.local/agent/internal/printer"
)

func (a *Agent) Worker(ctx context.Context) {
	defer close(a.workerDone)
	retry := func() bool {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return true
		}
	}
	for ctx.Err() == nil {
		job, err := a.store.ClaimNextJob(time.Now())
		if err != nil {
			a.RecordDiagnostic("error", "database", "Posao nije poslan: čitanje reda ili spremanje statusa nije uspjelo.", "")
			if !retry() {
				return
			}
			continue
		}
		if job == nil {
			select {
			case <-ctx.Done():
				return
			case <-a.wake:
				continue
			case <-time.After(5 * time.Second):
				continue
			}
		}
		a.RecordDiagnostic("info", "printer", "Slanje printeru je započelo.", job.ID)
		job.Status, job.Message = printer.Transmit(ctx, *job)
		job.UpdatedAt = time.Now().UTC()
		completed := job.UpdatedAt
		job.CompletedAt = &completed
		for {
			if err := a.store.FinishJob(job); err == nil {
				break
			}
			a.RecordDiagnostic("error", "database", "Ishod posla nije sačuvan. Red printanja čeka oporavak baze.", job.ID)
			job.Status = "unknown"
			job.Message = "Ishod slanja nije moguće sačuvati. Provjeri printer prije novog printanja."
			// Never resend the receipt. Keep only this active job until its uncertain
			// outcome is durable; a restart recovers the persisted 'sending' state.
			if !retry() {
				return
			}
		}
		level := "info"
		if job.Status == "failed" {
			level = "error"
		} else if job.Status == "unknown" {
			level = "warning"
		}
		a.RecordDiagnostic(level, "printer", job.Message, job.ID)
	}
}
