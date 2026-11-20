//go:build desktop && (darwin || windows)

package desktop

import (
	"time"
)

// This only hides developer UI. Diagnostic recording always remains active.
func (s *nativeStudio) clickVersion(now time.Time) {
	if s.diagnosticClicks == 0 || now.Sub(s.diagnosticClickStart) >= 5*time.Second || now.Before(s.diagnosticClickStart) {
		s.diagnosticClicks = 0
		s.diagnosticClickStart = now
	}
	s.diagnosticClicks++
	if s.diagnosticClicks < 7 {
		return
	}
	s.diagnosticClicks = 0
	s.diagnosticsVisible = !s.diagnosticsVisible
	message := "Dijagnostički prikaz je uključen."
	if !s.diagnosticsVisible {
		message = "Dijagnostički prikaz je isključen."
		if s.modal == "diagnostic" {
			s.modal = ""
		}
		if s.page == "Dijagnostika" {
			s.navigate("Printeri")
		}
	}
	s.agent.RecordDiagnostic("info", "agent", message, "")
	s.showMessage(message)
}
