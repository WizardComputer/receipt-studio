package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/storage"
)

// Run at startup and daily, including when the diagnostics screen is hidden.
// The caller waits for completion before closing the database.

func (a *Agent) StartDiagnosticMaintenance(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		a.maintainDiagnostics(ctx, ticker.C)
	}()
	return done
}

func (a *Agent) maintainDiagnostics(ctx context.Context, ticks <-chan time.Time) {
	a.cleanDiagnostics(ctx, time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now, ok := <-ticks:
			if !ok {
				return
			}
			a.cleanDiagnostics(ctx, now)
		}
	}
}

func (a *Agent) cleanDiagnostics(ctx context.Context, now time.Time) {
	count, err := a.store.PruneDiagnostics(ctx, now)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		a.RecordDiagnostic("error", "database", "Automatsko čišćenje dijagnostike nije uspjelo.", "")
	} else if count > 0 {
		a.RecordDiagnostic("info", "database", fmt.Sprintf("Obrisano %d dijagnostičkih zapisa starijih od 15 dana.", count), "")
	}
}

// Technical events contain fixed descriptions and job IDs, never request bodies or credentials.
func (a *Agent) RecordDiagnostic(level, component, message, jobID string) {
	if err := storage.WriteDiagnostic(a.db, level, component, message, jobID); err != nil {
		fmt.Fprintln(os.Stderr, "Receipt Studio: diagnostic could not be saved:", err)
	}
}

// Call after the action has returned and its transaction/locks have been released.
// Error text can contain payload values, so diagnostics use fixed, translated descriptions.
func (a *Agent) RecordActionFailure(component, message string, err error) {
	if err == nil {
		return
	}
	level := "error"
	var failure *model.ActionError
	if errors.As(err, &failure) {
		switch failure.Kind {
		case model.StorageFailure:
			component = "database"
		case model.InvalidInput, model.InvalidReceipt, model.NotFound, model.Conflict:
			level = "warning"
		}
	}
	a.RecordDiagnostic(level, component, message, "")
}
