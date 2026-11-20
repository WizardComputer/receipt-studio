//go:build desktop && (darwin || windows)

package desktop

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"receiptstudio.local/agent/internal/model"
)

type diagnosticFilters struct{ Level, Component, Search, From, To string }

type diagnosticResult struct {
	page model.DiagnosticPage
	err  error
	more bool
}

type nativeDiagnostics struct {
	draft, applied diagnosticFilters
	items          []model.Diagnostic
	before         int64
	selected       model.Diagnostic
	pending        chan diagnosticResult
	failure        string
}

var diagnosticLevels = map[string]string{"": "Svi nivoi", "info": "Informacija", "warning": "Upozorenje", "error": "Greška"}

var diagnosticComponents = map[string]string{"": "Sve komponente", "agent": "Agent", "api": "API", "printer": "Printer", "database": "Baza", "updater": "Nadogradnje"}

func (f diagnosticFilters) query() (model.DiagnosticQuery, error) {
	q := model.DiagnosticQuery{Level: f.Level, Component: f.Component, Search: f.Search}
	for i, value := range []string{f.From, f.To} {
		if value == "" {
			continue
		}
		parsed, err := time.ParseInLocation(jobFilterTimeFormat, value, time.Local)
		if err != nil {
			return q, fmt.Errorf("Vrijeme unesi kao YYYY-MM-DD HH:MM")
		}
		if i == 0 {
			q.From = parsed
		} else {
			q.To = parsed.Add(time.Minute)
		}
	}
	if !q.From.IsZero() && !q.To.IsZero() && !q.From.Before(q.To) {
		return q, fmt.Errorf("Početak raspona mora biti prije kraja.")
	}
	return q, nil
}

func (s *nativeStudio) loadDiagnostics(more bool) {
	d := &s.diagnostics
	if d.pending != nil {
		return
	}
	q, err := d.applied.query()
	if err != nil {
		s.showMessage(err.Error())
		return
	}
	if more {
		q.Before = d.before
	}
	result := make(chan diagnosticResult, 1)
	d.pending = result
	d.failure = ""
	agent := s.agent
	go func() { page, err := agent.ListDiagnostics(q); result <- diagnosticResult{page, err, more} }()
}

func (s *nativeStudio) receiveDiagnostics() {
	d := &s.diagnostics
	if d.pending == nil {
		return
	}
	select {
	case result := <-d.pending:
		d.pending = nil
		if result.err != nil {
			d.failure = result.err.Error()
			return
		}
		if result.more {
			d.items = append(d.items, result.page.Items...)
		} else {
			d.items = result.page.Items
		}
		d.before = result.page.NextBefore
	default:
	}
}

func diagnosticSelect(c *ui.Context, label string, value *string, keys []string, names map[string]string) {
	ui.Column(c).Width(155).Gap(7).Children(func() {
		studioLabel(c, label)
		selected := names[*value]
		options := make([]string, 0, len(keys))
		for _, key := range keys {
			options = append(options, names[key])
		}
		if studioSelect(c, &selected, options).Label(tr(c, label)).FillWidth().Height(42).Changed() {
			for _, key := range keys {
				if selected == names[key] {
					*value = key
				}
			}
		}
	})
}

func diagnosticBadge(c *ui.Context, level string) {
	bg, fg := ui.RGB(234, 243, 255), studioBlue
	if level == "warning" {
		bg, fg = ui.RGB(255, 245, 228), ui.RGB(167, 119, 32)
	}
	if level == "error" {
		bg, fg = ui.RGB(255, 240, 237), ui.RGB(193, 74, 57)
	}
	studioText(c, diagnosticLevels[level]).FontSize(10).PaddingX(7).PaddingY(5).Radius(5).Background(bg).TextColor(fg)
}

func (s *nativeStudio) diagnosticsView(c *ui.Context) {
	d := &s.diagnostics
	s.receiveDiagnostics()
	defer func() {
		if d.pending != nil {
			c.After(50 * time.Millisecond)
		}
	}()
	studioHeading(c, "Dijagnostika", "Tehnički događaji i greške agenta.", func() {
		if studioIconButton(c, "Osvježi dijagnostiku", "repeat").Disabled(d.pending != nil).Clicked() {
			s.loadDiagnostics(false)
		}
	})
	studioCard(c).Padding(20).Gap(14).Children(func() {
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.End).Children(func() {
			ui.Column(c).Width(0).Grow(1).MinWidth(0).Gap(7).Children(func() {
				studioLabel(c, "Pretraga")
				ui.TextInput(c, &d.draft.Search).Label(tr(c, "Pretraži poruke ili ID posla")).FillWidth().Height(42)
			})
			diagnosticSelect(c, "Nivo", &d.draft.Level, []string{"", "info", "warning", "error"}, diagnosticLevels)
			diagnosticSelect(c, "Komponenta", &d.draft.Component, []string{"", "agent", "api", "printer", "database", "updater"}, diagnosticComponents)
		})
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.End).Children(func() {
			ui.Column(c).Width(0).Grow(1).MinWidth(0).Gap(7).Children(func() { studioLabel(c, "Od"); studioDateTimePicker(c, &d.draft.From, "Dijagnostika od", false) })
			ui.Column(c).Width(0).Grow(1).MinWidth(0).Gap(7).Children(func() { studioLabel(c, "Do"); studioDateTimePicker(c, &d.draft.To, "Dijagnostika do", true) })
			ready := d.pending == nil && d.draft != d.applied
			if studioButton(c, "Primijeni", ready).Height(42).Disabled(!ready).Clicked() {
				if _, err := d.draft.query(); err != nil {
					s.showMessage(err.Error())
				} else {
					d.applied = d.draft
					d.items = nil
					d.before = 0
					s.loadDiagnostics(false)
				}
			}
			if studioIconButton(c, "Očisti filtere", "close").Size(42, 42).Disabled(d.pending != nil || d.draft == (diagnosticFilters{}) && d.applied == (diagnosticFilters{})).Clicked() {
				d.draft = diagnosticFilters{}
				d.applied = d.draft
				d.items = nil
				d.before = 0
				s.loadDiagnostics(false)
			}
		})
	})
	if d.failure != "" {
		studioText(c, d.failure).FillWidth().FontSize(12).TextColor(ui.RGB(193, 74, 57))
	}
	if len(d.items) == 0 {
		studioCard(c).Padding(40).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Icon(c, studioIcons["diagnostics"]).Size(34, 34).TextColor(studioMuted)
			if d.pending != nil {
				studioText(c, "Učitavanje…").FontSize(15)
			} else {
				studioText(c, "Nema dijagnostičkih zapisa za odabrane filtere.").FillWidth().TextAlign(ui.Center).FontSize(15)
			}
		})
	}
	if len(d.items) > 0 {
		studioCard(c).Padding(0).Gap(0).Children(func() {
			for _, entry := range d.items {
				b := ui.ButtonBase(c.Key(entry.ID)).Label(fmt.Sprintf("%s #%d", tr(c, "Detalji zapisa"), entry.ID)).FillWidth().PaddingX(20).PaddingY(16).Justify(ui.Start)
				if b.Hovered() {
					b.Background(studioBG)
				}
				b.Children(func() {
					ui.Column(c).FillWidth().Gap(9).Children(func() {
						ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
							diagnosticBadge(c, entry.Level)
							studioText(c, diagnosticComponents[entry.Component]).FontSize(11).TextColor(studioMuted).Grow(1)
							ui.Text(c, nativeTime(entry.CreatedAt)).FontSize(10).TextColor(studioMuted)
						})
						summary := strings.ReplaceAll(entry.Message, "\n", " ")
						if runes := []rune(summary); len(runes) > 160 {
							summary = string(runes[:160]) + "…"
						}
						studioText(c, summary).FillWidth().FontSize(12)
						if entry.JobID != "" {
							ui.Text(c, entry.JobID).FontSize(10).TextColor(studioMuted)
						}
					})
				})
				if b.Clicked() {
					entry, err := s.agent.DiagnosticDetails(entry.ID)
					if err != nil {
						s.showMessage(err.Error())
					} else {
						d.selected = entry
						s.modal = "diagnostic"
					}
				}
				ui.Divider(c)
			}
		})
	}
	if d.before > 0 && d.failure == "" {
		end := studioText(c, "Učitavanje…").Key("diagnostics-more").Padding(16).FontSize(11).TextColor(studioMuted)
		_, height := c.Size()
		if r := end.Bounds(); r.H > 0 && r.Y < height && r.Y+r.H > 0 {
			s.loadDiagnostics(true)
		}
	}
}

func diagnosticCopyText(d model.Diagnostic) string {
	return fmt.Sprintf("ID: %d\nTime: %s\nLevel: %s\nComponent: %s\nJob: %s\n\n%s", d.ID, d.CreatedAt.Format(time.RFC3339Nano), d.Level, d.Component, d.JobID, d.Message)
}

func (s *nativeStudio) diagnosticView(c *ui.Context) {
	d := s.diagnostics.selected
	ui.Row(c).FillWidth().Gap(10).Children(func() {
		diagnosticBadge(c, d.Level)
		studioText(c, diagnosticComponents[d.Component]).FontSize(12)
		ui.Text(c, nativeTime(d.CreatedAt)).FontSize(12).TextColor(studioMuted)
	})
	_, height := c.Size()
	ui.Scroll(c).FillWidth().Height(min(320, height-340)).Background(studioBG).Radius(10).Padding(16).Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			ui.Text(c, fmt.Sprintf("ID: %d", d.ID)).FontSize(11).TextColor(studioMuted)
			if d.JobID != "" {
				ui.Text(c, d.JobID).FontSize(11)
			}
			ui.Text(c, d.Message).FillWidth().FontSize(12)
		})
	})
	ui.Row(c).FillWidth().Justify(ui.End).Gap(9).Children(func() {
		if d.JobID != "" && studioButton(c, "Otvori posao", false).Clicked() {
			s.openJob(d.JobID)
		}
		if studioButton(c, "Kopiraj zapis", false).Clicked() {
			mygo.Clipboard.WriteText(diagnosticCopyText(d))
			s.showMessage("Zapis je kopiran.")
		}
		if studioButton(c, "Zatvori detalje", true).Clicked() {
			s.modal = ""
		}
	})
}
