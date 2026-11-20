package agent

import (
	"context"
	"io"
	"time"

	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/receipt"
	"receiptstudio.local/agent/internal/storage"
)

func (a *Agent) Close() error                                     { return a.db.Close() }
func (a *Agent) LogWriter() io.Writer                             { return storage.NewLog(a.db) }
func (a *Agent) APIKey() string                                   { a.mu.Lock(); defer a.mu.Unlock(); return a.config.APIKey }
func (a *Agent) Encoder() *receipt.Encoder                        { return a.encoder }
func (a *Agent) Origin() string                                   { return a.origin }
func (a *Agent) SetEndpoint(origin, host string)                  { a.origin, a.host = origin, host }
func (a *Agent) WorkerDone() <-chan struct{}                      { return a.workerDone }
func (a *Agent) Preferences() (model.Preferences, error)          { return a.store.Preferences() }
func (a *Agent) DesktopInitialized() bool                         { return a.store.DesktopInitialized() }
func (a *Agent) MarkDesktopInitialized() error                    { return a.store.MarkDesktopInitialized() }
func (a *Agent) JobStatus(id string) (*model.Job, error)          { return a.store.JobStatus(id) }
func (a *Agent) ListJobs(q model.JobQuery) (model.JobPage, error) { return a.store.ListJobs(q) }
func (a *Agent) ListDiagnostics(q model.DiagnosticQuery) (model.DiagnosticPage, error) {
	return a.store.ListDiagnostics(q)
}
func (a *Agent) DiagnosticDetails(id int64) (model.Diagnostic, error) {
	return a.store.DiagnosticDetails(id)
}
func (a *Agent) PruneDiagnostics(ctx context.Context, now time.Time) (int64, error) {
	return a.store.PruneDiagnostics(ctx, now)
}
