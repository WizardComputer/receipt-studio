//go:build desktop && (darwin || windows)

package desktop

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"receiptstudio.local/agent/internal/model"
)

func (s *nativeStudio) openTemplateHistory(t model.ReceiptTemplate) {
	page, err := s.agent.TemplateHistory(t.ID, 0)
	if err != nil {
		s.showMessage(err.Error())
		return
	}
	s.historyTemplate = t
	s.history = page
	s.historyTab = "Pregled"
	s.historyVersion = model.TemplateRevision{}
	s.historyError = ""
	s.modal = "history"
	if len(page.Versions) > 0 {
		s.selectTemplateVersion(page.Versions[0].Version)
	}
}

func (s *nativeStudio) selectTemplateVersion(version int) {
	s.historyError = ""
	s.historyBitmap = nil
	s.historyPreviewLoaded = false
	s.historyVersion = model.TemplateRevision{}
	var err error
	s.historyVersion, err = s.agent.TemplateVersion(s.historyTemplate.ID, version)
	if err != nil {
		s.historyError = err.Error()
	}
}

func (s *nativeStudio) templateHistoryView(c *ui.Context) {
	_, height := c.Size()
	ui.Row(c).FillWidth().Height(min(470, height-240)).Gap(20).Children(func() {
		ui.Scroll(c).Width(205).FillHeight().Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				for _, v := range s.history.Versions {
					b := ui.ButtonBase(c.Key(v.Version)).Label(fmt.Sprintf("Verzija %d", v.Version)).FillWidth().Padding(12).Radius(9).Border(1, studioLine)
					if s.historyVersion.Version == v.Version {
						b.Background(ui.RGB(237, 245, 255))
					} else {
						b.Background(studioWhite)
					}
					b.Children(func() {
						ui.Column(c).FillWidth().Gap(6).Children(func() {
							label := fmt.Sprintf("Verzija %d", v.Version)
							if v.Version == s.historyTemplate.Version {
								label += " · trenutna"
							}
							studioText(c, label).FontSize(12).FontWeight(600)
							studioText(c, nativeTime(v.CreatedAt)).FontSize(10).TextColor(studioMuted)
							if v.RestoredFrom > 0 {
								studioText(c, fmt.Sprintf("Vraćena iz v%d", v.RestoredFrom)).FontSize(10).TextColor(studioMuted)
							}
						})
					})
					if b.Clicked() {
						s.selectTemplateVersion(v.Version)
					}
				}
				if s.history.NextBefore > 0 && studioButton(c, "Učitaj starije", false).Clicked() {
					page, err := s.agent.TemplateHistory(s.historyTemplate.ID, s.history.NextBefore)
					if err != nil {
						s.historyError = err.Error()
					} else {
						s.history.Versions = append(s.history.Versions, page.Versions...)
						s.history.NextBefore = page.NextBefore
					}
				}
			})
		})
		ui.Column(c).Width(0).Grow(1).MinWidth(0).FillHeight().Gap(12).Children(func() {
			studioTabs(c, &s.historyTab, []string{"Pregled", "Dizajn", "Podaci"})
			ui.Scroll(c).Key(fmt.Sprintf("version-%d-%s", s.historyVersion.Version, s.historyTab)).FillWidth().Height(0).Grow(1).MinHeight(0).Background(studioBG).Radius(10).Padding(14).Children(func() {
				if s.historyVersion.Version == 0 {
					studioText(c, s.historyError).FillWidth()
					return
				}
				switch s.historyTab {
				case "Dizajn":
					studioCode(c, "Dizajn verzije", prettyJSON(s.historyVersion.Template))
				case "Podaci":
					studioCode(c, "Podaci verzije", prettyJSON(s.historyVersion.SampleData))
				default:
					if !s.historyPreviewLoaded {
						s.historyPreviewLoaded = true
						png, err := s.agent.TemplateVersionPreview(s.historyTemplate.ID, s.historyVersion.Version)
						if err != nil {
							s.historyError = err.Error()
						} else {
							var err error
							s.historyBitmap, err = ui.DecodeBitmap(png.Data)
							if err != nil {
								s.historyError = err.Error()
							}
						}
					}
					if s.historyBitmap != nil {
						ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() { ui.Image(c, s.historyBitmap).Width(360) })
					} else {
						studioText(c, s.historyError).FillWidth().FontSize(12)
					}
				}
			})
		})
	})
	ui.Row(c).FillWidth().Justify(ui.End).Gap(9).Children(func() {
		if studioButton(c, "Zatvori historiju", false).Clicked() {
			s.closeModal()
		}
		if studioButton(c, "Vrati ovu verziju", true).Disabled(s.historyVersion.Version == 0 || s.historyVersion.Version == s.historyTemplate.Version).Clicked() {
			target := s.historyVersion.Version
			current := s.historyTemplate
			s.confirm(fmt.Sprintf("Vratiti verziju %d? Bit će sačuvana kao nova verzija %d; postojeća historija ostaje sačuvana.", target, current.Version+1), func() {
				restored, err := s.agent.RestoreTemplateVersion(current.ID, target, current.Version)
				if err != nil {
					s.showMessage(err.Error())
					return
				}
				s.reload()
				s.openTemplateHistory(restored)
				s.showMessage(fmt.Sprintf("Verzija %d je vraćena kao v%d.", target, restored.Version))
			})
		}
	})
}
