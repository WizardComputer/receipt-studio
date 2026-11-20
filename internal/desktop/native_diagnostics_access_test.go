//go:build desktop && (darwin || windows)

package desktop

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	resources "receiptstudio.local/agent"
	"receiptstudio.local/agent/internal/model"
)

func TestNativeDiagnosticsSevenClicksToggle(t *testing.T) {
	a := testAgent(t)
	s := newNativeStudio(a)
	tt := ui.NewTester(s.view, 1000, 650)
	if _, ok := tt.Find("Dijagnostika"); ok {
		t.Fatal("diagnostics starts visible")
	}
	if _, ok := tt.Find("v" + resources.AppVersion); !ok {
		t.Fatal("sidebar missing application version")
	}
	s.navigate("Dijagnostika")
	if s.page != "Printeri" {
		t.Fatal("hidden diagnostics remains navigable")
	}
	s.navigate("Predlošci")
	tt.Frame()
	for i := 0; i < 6; i++ {
		if err := tt.Click("Verzija aplikacije"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if s.diagnosticsVisible || s.page != "Predlošci" {
			t.Fatal("partial gesture toggled or navigated")
		}
	}
	if err := tt.Click("Verzija aplikacije"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("Dijagnostika"); !ok {
		t.Fatal("seven clicks did not reveal diagnostics")
	}
	lastDiagnostic(t, a, "info", "agent", "Dijagnostički prikaz je uključen.")
	if err := tt.Click("Dijagnostika"); err != nil {
		t.Fatal(err)
	}
	waitDiagnostics(t, s, tt)
	for i := 0; i < 7; i++ {
		if err := tt.Click("Verzija aplikacije"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	if s.diagnosticsVisible || s.page != "Printeri" {
		t.Fatal("second gesture did not hide diagnostics and return home")
	}
	if _, ok := tt.Find("Dijagnostika"); ok {
		t.Fatal("hidden menu remains rendered")
	}
	lastDiagnostic(t, a, "info", "agent", "Dijagnostički prikaz je isključen.")
	if err := a.SavePreferences(model.Preferences{Language: "en"}); err != nil {
		t.Fatal(err)
	}
	lastDiagnostic(t, a, "info", "agent", "Jezik interfejsa je promijenjen")
	// Opening another UI session keeps the diagnostics menu hidden.
	if newNativeStudio(a).diagnosticsVisible {
		t.Fatal("visibility persisted")
	}
	s.language = "en"
	tt.Frame()
	for i := 0; i < 7; i++ {
		if err := tt.Click("Application version"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	if _, ok := tt.Find("Diagnostics"); !ok {
		t.Fatal("English toggle did not reveal diagnostics")
	}
	if _, ok := tt.Find("Diagnostics view enabled."); !ok {
		t.Fatal("toggle toast not translated")
	}
	s.navigate("Predlošci")
	tt.Frame()
	if err := tt.Click("Receipt Studio"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.page != "Printeri" {
		t.Fatal("brand no longer navigates home")
	}
}

func TestNativeDiagnosticsClickWindow(t *testing.T) {
	s := newNativeStudio(testAgent(t))
	start := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		s.clickVersion(start.Add(time.Duration(i) * time.Second / 2))
	}
	s.clickVersion(start.Add(5 * time.Second))
	if s.diagnosticsVisible || s.diagnosticClicks != 1 {
		t.Fatal("expired sequence counted")
	}
	for i := 1; i < 7; i++ {
		s.clickVersion(start.Add(5*time.Second + time.Duration(i)*time.Millisecond))
	}
	if !s.diagnosticsVisible || s.diagnosticClicks != 0 {
		t.Fatal("fresh seven-click sequence did not toggle")
	}
}

func TestNativeUpdatePreviewUsesApplicationVersion(t *testing.T) {
	s := newNativeStudio(testAgent(t))
	s.updateDemo, s.updateDemoOpen = true, true
	s.language = "en"
	tt := ui.NewTester(s.view, 1000, 650)
	for _, label := range []string{"Receipt Studio " + demoUpdateVersion(), "Current version: " + resources.AppVersion} {
		if _, ok := tt.Find(label); !ok {
			t.Fatal("update preview missing version", label)
		}
	}
	s.apiTab = "Status agenta"
	_, _, response := s.apiExample(false)
	if !strings.Contains(response, `"app_version": "`+resources.AppVersion+`"`) {
		t.Fatal("API example missing current application version", response)
	}
}
