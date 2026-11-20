//go:build desktop && (darwin || windows)

package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	resources "receiptstudio.local/agent"
	"receiptstudio.local/agent/internal/agent"
	"receiptstudio.local/agent/internal/i18n"
	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/printer"
)

type nativeStudio struct {
	diagnosticsVisible                                      bool
	diagnosticClicks                                        int
	diagnosticClickStart                                    time.Time
	diagnostics                                             nativeDiagnostics
	agent                                                   *agent.Agent
	window                                                  *mygo.Window
	config                                                  model.Config
	page, message, modal, confirmText                       string
	messageUntil                                            time.Time
	confirmAction                                           func()
	editor                                                  *nativeEditor
	editing                                                 model.ReceiptTemplate
	saved                                                   string
	printer                                                 model.Printer
	queues                                                  []string
	jobs                                                    []map[string]any
	cursor                                                  string
	status, from, to, appliedStatus, appliedFrom, appliedTo string
	jobsLoading                                             bool
	job                                                     map[string]any
	jobTab                                                  string
	jobBitmap                                               *ui.Bitmap
	apiTab                                                  string
	quitting                                                bool
	showKey                                                 bool
	defaultExampleData                                      json.RawMessage
	counts                                                  map[string]int
	historyTemplate                                         model.ReceiptTemplate
	history                                                 model.RevisionPage
	historyVersion                                          model.TemplateRevision
	historyTab, historyError                                string
	historyBitmap                                           *ui.Bitmap
	historyPreviewLoaded                                    bool
	language                                                string
	onLanguageChanged                                       func(string)
	updateDemo, updateDemoOpen                              bool
}

func newNativeStudio(a *agent.Agent) *nativeStudio {
	s := &nativeStudio{agent: a, page: "Printeri", apiTab: "Slanje računa", status: "Svi", appliedStatus: "Svi", language: "bs"}
	if p, err := a.Preferences(); err == nil {
		s.language = p.Language
	}
	_, s.defaultExampleData, _ = a.Encoder().Defaults()
	s.reload()
	return s
}

func (s *nativeStudio) post(fn func()) {
	mygo.RunOnMain(func() {
		fn()
		if s.window != nil {
			s.window.Invalidate()
		}
	})
}

// Restart the timeout for every notification, including repeated messages.
func (s *nativeStudio) showMessage(message string) {
	s.message = message
	s.messageUntil = time.Now().Add(5 * time.Second)
	if s.window != nil {
		s.window.Invalidate()
	}
}

func (s *nativeStudio) reload() {
	s.config = s.agent.ConfigSnapshot()
}

func (s *nativeStudio) dirty() bool {
	return s.editor != nil && s.saved != string(s.editor.doc.JSON())+string(s.editor.doc.Data)
}

func (s *nativeStudio) confirm(text string, fn func()) { s.confirmText = text; s.confirmAction = fn }

func (s *nativeStudio) navigate(page string) {
	if page == "Dijagnostika" && !s.diagnosticsVisible {
		return
	}
	goTo := func() {
		s.page = page
		s.editor = nil
		s.reload()
		if page == "Dijagnostika" {
			s.loadDiagnostics(false)
		}
		if page == "Poslovi printanja" {
			s.cursor = ""
			s.loadJobs(false)
		}
	}
	if s.dirty() {
		s.confirm("Odbaciti nesačuvane izmjene predloška?", goTo)
	} else {
		goTo()
	}
}

func (s *nativeStudio) openEditor(t model.ReceiptTemplate) {
	e, err := newNativeEditor(s.agent.Encoder(), t.Template, t.SampleData)
	if err != nil {
		s.showMessage(err.Error())
		return
	}
	s.editing = t
	s.editor = e
	s.page = "Editor"
	s.saved = string(e.doc.JSON()) + string(e.doc.Data)
	e.post = s.post
	e.language = s.language
	e.languageToggle = s.languageToggle
	e.onClose = func() { s.navigate("Predlošci") }
	e.onSave = func(raw, data json.RawMessage) error {
		value := s.editing
		value.Template = raw
		value.SampleData = data
		result, err := s.agent.SaveTemplate(value, false)
		if err != nil {
			return err
		}
		s.editing = result
		s.saved = string(raw) + string(data)
		s.showMessage("Predložak je sačuvan.")
		s.reload()
		return nil
	}
}

func (s *nativeStudio) view(c *ui.Context) {
	*nativeLanguage(c) = s.language
	c.SetTheme(&studioTheme)
	width, _ := c.Size()
	sidebar, inset := float32(232), float32(44)
	if width <= 1100 {
		sidebar, inset = 200, 26
	}
	if s.page == "Editor" && s.editor != nil {
		ui.Column(c).Fill().Background(studioWhite).Children(func() {
			s.editor.view(c)
		})
	} else {
		ui.Row(c).Fill().Background(studioBG).Children(func() {
			ui.Column(c).Width(sidebar).FillHeight().PaddingX(18).PaddingY(30).Gap(5).Background(ui.RGB(238, 238, 241)).Border(1, ui.RGB(225, 225, 230)).Children(func() {
				studioText(c, "RADNI PROSTOR").PaddingX(12).PaddingY(10).FontSize(9).TextColor(studioMuted)
				for i, name := range []string{"Printeri", "Predlošci", "Poslovi printanja", "API povezivanje", "Dijagnostika"} {
					if name == "Dijagnostika" && !s.diagnosticsVisible {
						continue
					}
					icon := []string{"printer", "template", "jobs", "api", "diagnostics"}[i]
					b := ui.ButtonBase(c.Key(name)).Label(tr(c, name)).FillWidth().Height(43).PaddingX(12).Gap(11).Radius(8).Justify(ui.Start)
					color := studioMuted
					if s.page == name {
						b.Background(studioWhite).Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, .04))
						color = studioBlue
					} else if b.Hovered() {
						b.Background(ui.RGB(230, 230, 235))
					}
					b.Children(func() {
						ui.Icon(c, studioIcons[icon]).Size(20, 20).TextColor(color)
						studioText(c, name).FontSize(12).FontWeight(500).Grow(1)
						if name == "Printeri" {
							studioText(c, fmt.Sprint(len(s.config.Printers))).FontSize(10).TextColor(studioMuted)
						}
					})
					if b.Clicked() {
						s.navigate(name)
					}
				}
				ui.Box(c).Height(0).Grow(1)
				if s.updateDemo {
					if studioButton(c, fmt.Sprintf("Nova verzija · %s", demoUpdateVersion()), false).FillWidth().Clicked() {
						s.updateDemoOpen = true
					}
				}
				ui.Row(c).Key("branding").FillWidth().Height(28).PaddingX(12).Gap(6).AlignItems(ui.Center).Children(func() {
					brand := ui.ButtonBase(c.Key("home")).Label(tr(c, "Receipt Studio")).Height(28)
					brand.Children(func() {
						studioText(c, "Receipt Studio").FontSize(11).FontWeight(500).TextColor(studioMuted).SingleLine()
					})
					if brand.Clicked() {
						s.navigate("Printeri")
					}
					version := ui.ButtonBase(c.Key("version")).Label(tr(c, "Verzija aplikacije")).Height(28).PaddingX(2)
					version.Children(func() { ui.Text(c, "v"+resources.AppVersion).FontSize(9).TextColor(studioMuted).SingleLine() })
					if version.Clicked() {
						s.clickVersion(time.Now())
					}
				})
			})
			ui.Column(c).Width(0).Grow(1).FillHeight().MinWidth(0).Children(func() {
				ui.Row(c).FillWidth().Height(67).PaddingX(inset).AlignItems(ui.Center).Background(ui.RGB(250, 250, 251)).Border(1, studioLine).Children(func() {
					ui.Text(c, tr(c, "Radni prostor")+" / "+tr(c, s.page)).FontSize(11).TextColor(studioMuted).Grow(1)
					s.languageToggle(c)
				})
				if s.page == "Poslovi printanja" || s.page == "Dijagnostika" {
					ui.Scroll(c).Key(s.page).FillWidth().Height(0).Grow(1).MinHeight(0).Children(func() {
						ui.Column(c).FillWidth().PaddingX(inset).PaddingY(46).Gap(32).Children(func() {
							if s.page == "Dijagnostika" {
								s.diagnosticsView(c)
							} else {
								s.jobsView(c)
							}
						})
					})
				} else {
					ui.Column(c).Fill().PaddingX(inset).PaddingY(46).Gap(32).Children(func() {
						switch s.page {
						case "Printeri":
							s.printersView(c)
						case "Predlošci":
							s.templatesView(c)
						case "API povezivanje":
							s.apiView(c)
						}
					})
				}
			})
		})
	}
	if s.modal != "" {
		open := true
		title, eyebrow, modalWidth := "Postavke printera", "PODEŠAVANJE", float32(510)
		if s.modal == "printer" && s.printer.ID == "" {
			title = "Dodaj printer"
		}
		if s.modal == "job" {
			title, eyebrow, modalWidth = "Detalji računa", "POSAO PRINTANJA", 780
		}
		if s.modal == "diagnostic" {
			title, eyebrow, modalWidth = "Detalji zapisa", "DIJAGNOSTIKA", 700
		}
		if s.modal == "history" {
			title, eyebrow, modalWidth = nativeTemplateName(s.historyTemplate), "HISTORIJA PREDLOŠKA", 920
		}
		if s.modal != "history" {
			title = tr(c, title)
		}
		studioModal(c, "studio-modal", eyebrow, title, modalWidth, &open, func() {
			if s.modal == "printer" {
				s.printerForm(c)
			} else if s.modal == "diagnostic" {
				s.diagnosticView(c)
			} else if s.modal == "history" {
				s.templateHistoryView(c)
			} else {
				s.jobView(c)
			}
		})
		if !open {
			s.closeModal()
		}
	}
	if s.confirmAction != nil {
		open := true
		studioModal(c, "confirmation", "POTVRDA", tr(c, "Potvrdi radnju"), 440, &open, func() {
			studioText(c, s.confirmText).FillWidth().FontSize(13).TextColor(studioMuted)
			ui.Row(c).FillWidth().Justify(ui.End).Gap(9).Children(func() {
				if studioButton(c, "Odustani", false).Clicked() {
					open = false
				}
				if studioButton(c, "Potvrdi", true).Clicked() {
					fn := s.confirmAction
					s.confirmAction = nil
					fn()
				}
			})
		})
		if !open {
			s.confirmAction = nil
		}
	}
	if s.message != "" {
		if remaining := time.Until(s.messageUntil); remaining <= 0 {
			s.message = ""
		} else {
			c.After(remaining)
		}
	}
	if s.message != "" {
		ui.Row(c).Attach(ui.AnchorTop, ui.AnchorTop).Top(20).MaxWidth(560).Padding(14).Gap(12).AlignItems(ui.Center).Background(studioWhite).Border(1, studioLine).Radius(12).Shadow(0, 4, 18, 0, ui.RGBA(0, 0, 0, .12)).Children(func() {
			studioText(c, s.message).FontSize(12).MaxWidth(470)
			if studioIconButton(c, "Zatvori obavijest", "close").Size(26, 26).Clicked() {
				s.message = ""
			}
		})
	}
	s.updateDemoView(c)
}

func studioHeading(c *ui.Context, title, subtitle string, actions func()) {
	ui.Row(c).FillWidth().Gap(18).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Width(0).Grow(1).MinWidth(0).Gap(12).Children(func() {
			studioText(c, "R E C E I P T  S T U D I O").FontSize(9).FontWeight(600).TextColor(studioMuted)
			studioText(c, title).FontSize(34).FontWeight(650)
			studioText(c, subtitle).FillWidth().FontSize(13).TextColor(studioMuted)
		})
		if actions != nil {
			ui.Row(c).Gap(8).Children(actions)
		}
	})
}

func (s *nativeStudio) addPrinter() {
	s.printer = model.Printer{Connection: model.Connection{Kind: "tcp"}, Port: 9100}
	if len(s.config.Templates) > 0 {
		s.printer.TemplateID = s.config.Templates[0].ID
	}
	s.modal = "printer"
}

func (s *nativeStudio) printersView(c *ui.Context) {
	studioHeading(c, "Tvoji printeri.", "Svaki printer ima svoj način povezivanja i odabrani predložak iz biblioteke.", func() {
		if len(s.config.Printers) > 0 && studioButton(c, "Dodaj printer", true).Clicked() {
			s.addPrinter()
		}
	})
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Gap(14).Children(func() {
			if len(s.config.Printers) == 0 {
				studioCard(c).PaddingY(46).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, studioIcons["printer"]).Size(36, 36).TextColor(studioMuted)
					studioText(c, "Prvi račun počinje ovdje.").FontSize(15).FontWeight(600)
					studioText(c, "Odaberi LAN ili USB vezu. Primjer predloška bit će spreman za uređivanje.").FontSize(12).TextColor(studioMuted)
					if studioButton(c, "Dodaj prvi printer", true).Clicked() {
						s.addPrinter()
					}
				})
			}
			for _, p := range s.config.Printers {
				printer := p
				studioCard(c).Key(p.ID).Children(func() {
					ui.Row(c).FillWidth().Gap(20).AlignItems(ui.Center).Children(func() {
						studioGlyph(c, "printer")
						ui.Column(c).Width(0).Grow(1).MinWidth(0).Gap(9).Children(func() {
							ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
								ui.Text(c, p.Name).FontSize(15).FontWeight(600)
								if s.config.DefaultPrinter == p.ID {
									studioText(c, "Podrazumijevani").FontSize(9).Padding(4).Radius(5).Background(studioBG).TextColor(studioMuted)
								}
							})
							address := fmt.Sprintf("LAN · %s:%d", p.Host, p.Port)
							if p.Kind == "usb" {
								address = "USB · " + p.Queue
							}
							for _, t := range s.config.Templates {
								if t.ID == p.TemplateID {
									address += " · " + nativeTemplateSummary(t) + " · " + nativeTemplateName(t)
								}
							}
							ui.Text(c, address).FontSize(11).TextColor(studioMuted).FillWidth()
						})
						ui.Row(c).Gap(7).Children(func() {
							if studioIconButton(c, "Probni račun", "printer").Clicked() {
								s.confirm("Isprintati probni račun na printeru „"+printer.Name+"“?", func() {
									if _, err := s.agent.PrintTestReceipt(printer.ID); err != nil {
										s.showMessage(err.Error())
									} else {
										s.navigate("Poslovi printanja")
									}
								})
							}
							if studioIconButton(c, "Uredi predložak", "templateEdit").Clicked() {
								for _, t := range s.config.Templates {
									if t.ID == printer.TemplateID {
										s.openEditor(t)
										break
									}
								}
							}
							if studioIconButton(c, "Postavke", "settings").Clicked() {
								s.printer = printer
								s.modal = "printer"
								if printer.Kind == "usb" {
									s.discover()
								}
							}
						})
					})
				})
			}
		})
	})
}

func nativeTemplateSummary(t model.ReceiptTemplate) string {
	var v map[string]any
	_ = json.Unmarshal(t.Template, &v)
	paper := "80 mm"
	if v["profile"] == "narrow" {
		paper = "58 mm"
	}
	protocol := map[string]string{"esc-pos": "ESC/POS", "star-prnt": "StarPRNT", "star-line": "Star Line"}[textValue(v["language"])]
	if protocol == "" {
		protocol = "ESC/POS"
	}
	return paper + " · " + protocol
}

func (s *nativeStudio) discover() {
	go func() {
		result := printer.DiscoverLocalDevices(context.Background())
		s.post(func() {
			s.queues = result.USB
			if len(result.Warnings) > 0 {
				s.showMessage(result.Warnings[0])
			}
		})
	}()
}

func (s *nativeStudio) printerForm(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(9).Children(func() {
		p := &s.printer
		studioLabel(c, "Naziv printera")
		ui.TextInput(c, &p.Name).Label(tr(c, "Naziv printera")).Height(40).FillWidth()
		studioLabel(c, "Način povezivanja")
		kind := "LAN"
		if p.Kind == "usb" {
			kind = "USB"
		}
		if ui.Select(c, &kind, []string{"LAN", "USB"}).Label(tr(c, "Način povezivanja")).Height(40).FillWidth().Changed() {
			if kind == "USB" {
				p.Kind = "usb"
				s.discover()
			} else {
				p.Kind = "tcp"
			}
		}
		if p.Kind == "usb" {
			studioLabel(c, "Sistemski printer")
			studioText(c, "Prvo dodaj USB printer u postavkama operativnog sistema, pa odaberi njegov naziv.").FontSize(11).TextColor(studioMuted).FillWidth()
			ui.Row(c).Gap(8).Children(func() {
				options := append([]string{}, s.queues...)
				found := false
				for _, q := range options {
					if q == p.Queue {
						found = true
					}
				}
				if p.Queue != "" && !found {
					options = append(options, p.Queue)
				}
				ui.Select(c, &p.Queue, options).Width(0).Grow(1).Height(40).Label(tr(c, "Sistemski printer"))
				if studioIconButton(c, "Osvježi", "repeat").Clicked() {
					s.discover()
				}
			})
		} else {
			ui.Row(c).FillWidth().Gap(12).Children(func() {
				ui.Column(c).Width(0).Grow(1).Gap(7).Children(func() {
					studioLabel(c, "IP adresa")
					ui.TextInput(c, &p.Host).Height(40).FillWidth().Label(tr(c, "IP adresa"))
				})
				ui.Column(c).Width(105).Gap(7).Children(func() {
					studioLabel(c, "Port")
					port := float64(p.Port)
					if studioNumber(c, &port, 1, 65535, 1).Height(40).Label(tr(c, "Port")).Changed() {
						p.Port = int(port)
					}
				})
			})
		}
		studioLabel(c, "Predložak računa")
		options := []string{}
		selected := ""
		for _, t := range s.config.Templates {
			name := nativeTemplateName(t) + " · " + nativeTemplateSummary(t)
			for _, other := range s.config.Templates {
				if other.ID != t.ID && nativeTemplateName(other) == nativeTemplateName(t) && nativeTemplateSummary(other) == nativeTemplateSummary(t) {
					name += " · " + t.ID
					break
				}
			}
			options = append(options, name)
			if t.ID == p.TemplateID {
				selected = name
			}
		}
		if ui.Select(c, &selected, options).Height(40).FillWidth().Label(tr(c, "Predložak računa")).Changed() {
			for i, name := range options {
				if name == selected {
					p.TemplateID = s.config.Templates[i].ID
				}
			}
		}
		ui.Box(c).Height(10)
		ui.Divider(c)
		ui.Row(c).FillWidth().Gap(9).PaddingY(10).Children(func() {
			if studioButton(c, "Testiraj vezu", false).Clicked() {
				printer := *p
				go func() {
					message, err := s.agent.ProbePrinter(context.Background(), printer)
					s.post(func() {
						if err != nil {
							s.showMessage(err.Error())
						} else {
							s.showMessage(message)
						}
					})
				}()
			}
			ui.Box(c).Width(0).Grow(1)
			if studioButton(c, "Odustani", false).Clicked() {
				s.modal = ""
			}
			if studioButton(c, "Sačuvaj", true).Clicked() {
				printer := *p
				create := printer.ID == ""
				if create {
					printer.ID = "printer-" + model.Secret()[:12]
				}
				if _, err := s.agent.SavePrinter(printer, create); err != nil {
					s.showMessage(err.Error())
				} else {
					s.modal = ""
					s.reload()
				}
			}
		})
	})
}

func nativeTemplateName(t model.ReceiptTemplate) string {
	var content map[string]any
	_ = json.Unmarshal(t.Template, &content)
	return textValue(content["name"])
}

func (s *nativeStudio) createTemplate() {
	t, d, err := s.agent.Encoder().Defaults()
	if err != nil {
		s.showMessage(err.Error())
		return
	}
	entry := model.ReceiptTemplate{ID: "template-" + model.Secret()[:12], Template: t, SampleData: d}
	saved, err := s.agent.SaveTemplate(entry, true)
	if err != nil {
		s.showMessage(err.Error())
	} else {
		s.reload()
		s.openEditor(saved)
	}
}

func (s *nativeStudio) templatesView(c *ui.Context) {
	studioHeading(c, "Biblioteka predložaka.", "Jedan predložak može koristiti više printera. Izvezi ga i prenesi na drugi agent.", func() {
		if len(s.config.Templates) == 0 {
			return
		}
		if studioButton(c, "Novi predložak", true).Clicked() {
			s.createTemplate()
		}
		if studioButton(c, "Uvezi JSON", false).Clicked() {
			go s.importTemplate(s.language)
		}
	})
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			if len(s.config.Templates) == 0 {
				studioCard(c).PaddingY(46).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, studioIcons["template"]).Size(36, 36).TextColor(studioMuted)
					studioText(c, "Još nema predložaka.").FontSize(15).FontWeight(600)
					studioText(c, "Kreiraj predložak računa ili uvezi postojeći s drugog agenta.").FillWidth().TextAlign(ui.Center).FontSize(12).TextColor(studioMuted)
					ui.Row(c).Gap(9).Children(func() {
						if studioButton(c, "Kreiraj prvi predložak", true).Clicked() {
							s.createTemplate()
						}
						if studioButton(c, "Uvezi JSON", false).Clicked() {
							go s.importTemplate(s.language)
						}
					})
				})
			}
			for _, t := range s.config.Templates {
				entry := t
				ui.Row(c).Key(t.ID).FillWidth().Padding(24).Gap(20).AlignItems(ui.Center).Background(studioWhite).Border(1, studioLine).Radius(16).Children(func() {
					studioGlyph(c, "template")
					ui.Column(c).Width(0).Grow(1).MinWidth(0).Gap(9).Children(func() {
						studioText(c, fmt.Sprintf("%s · v%d", nativeTemplateName(t), t.Version)).FontSize(15).FontWeight(600)
						used := []string{}
						for _, p := range s.config.Printers {
							if p.TemplateID == t.ID {
								used = append(used, p.Name)
							}
						}
						usage := strings.Join(used, ", ")
						if usage == "" {
							usage = tr(c, "Nije dodijeljen printeru")
						}
						ui.Text(c, nativeTemplateSummary(t)+" · "+usage).FillWidth().FontSize(11).TextColor(studioMuted)
					})
					if studioIconButton(c, "Uredi", "templateEdit").Clicked() {
						s.openEditor(entry)
					}
					if studioIconButton(c, "Historija verzija", "history").Clicked() {
						s.openTemplateHistory(entry)
					}
					if studioIconButton(c, "Izvezi", "download").Clicked() {
						go s.exportTemplate(entry, s.language)
					}
					if studioIconButton(c, "Kopiraj", "copy").Clicked() {
						copied := entry
						copied.ID = "template-" + model.Secret()[:12]
						copied.Version = 0
						var content map[string]any
						_ = json.Unmarshal(copied.Template, &content)
						content["name"] = nativeTemplateName(entry) + " (kopija)"
						copied.Template, _ = json.Marshal(content)
						saved, err := s.agent.SaveTemplate(copied, true)
						if err != nil {
							s.showMessage(err.Error())
						} else {
							s.reload()
							s.openEditor(saved)
						}
					}
					assigned := false
					for _, p := range s.config.Printers {
						if p.TemplateID == t.ID {
							assigned = true
						}
					}
					if studioIconButton(c, "Obriši", "trash").Disabled(assigned).Clicked() {
						s.confirm("Obrisati predložak „"+nativeTemplateName(entry)+"“?", func() {
							if err := s.agent.DeleteTemplate(entry.ID); err != nil {
								s.showMessage(err.Error())
							} else {
								s.reload()
							}
						})
					}
				})
			}
		})
	})
}

func (s *nativeStudio) importTemplate(language string) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: i18n.Translate(language, "Uvezi predložak"), Filters: []mygo.FileFilter{{Name: i18n.Translate(language, "Predložak"), Extensions: []string{"json"}}}})
	if err != nil || len(paths) == 0 {
		return
	}
	f, err := os.Open(paths[0])
	if err != nil {
		s.agent.RecordActionFailure("agent", "Uvoz predloška nije uspio.", err)
		s.post(func() { s.showMessage(err.Error()) })
		return
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 1000001))
	if err != nil || len(b) > 1000000 {
		if err != nil {
			s.agent.RecordActionFailure("agent", "Uvoz predloška nije uspio.", err)
		} else {
			s.agent.RecordDiagnostic("warning", "agent", "Uvoz predloška nije uspio.", "")
		}
		s.post(func() { s.showMessage("Predložak je prevelik ili nije čitljiv.") })
		return
	}
	_, err = s.agent.ImportTemplate(b)
	s.post(func() {
		if err != nil {
			s.showMessage(err.Error())
		} else {
			s.reload()
		}
	})
}

func (s *nativeStudio) exportTemplate(t model.ReceiptTemplate, language string) {
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Title: i18n.Translate(language, "Izvezi predložak"), DefaultPath: "predlozak.json"})
	if err != nil || path == "" {
		return
	}
	value, err := s.agent.ExportTemplate(t.ID)
	if err == nil {
		b, _ := json.MarshalIndent(value, "", "  ")
		err = os.WriteFile(path, b, 0600)
	}
	if err == nil {
		s.agent.RecordDiagnostic("info", "agent", fmt.Sprintf("Predložak je izvezen: %s.", t.ID), "")
	} else {
		s.agent.RecordActionFailure("agent", "Izvoz predloška nije uspio.", err)
	}
	s.post(func() {
		if err != nil {
			s.showMessage(err.Error())
		} else {
			s.showMessage("Predložak je izvezen s primjerom podataka.")
		}
	})
}

func (s *nativeStudio) loadJobs(more bool) {
	if s.jobsLoading {
		return
	}
	s.jobsLoading = true
	q := model.JobQuery{Limit: 100}
	if more {
		q.Before = s.cursor
	}
	if s.appliedStatus != "Svi" {
		q.Status = s.appliedStatus
	}
	for key, value := range map[string]string{"from": s.appliedFrom, "to": s.appliedTo} {
		if value != "" {
			t, err := time.ParseInLocation(jobFilterTimeFormat, value, time.Local)
			if err != nil {
				s.jobsLoading = false
				s.showMessage("Vrijeme unesi kao YYYY-MM-DD HH:MM")
				return
			}
			if key == "to" {
				t = t.Add(time.Minute - time.Nanosecond)
			}
			if key == "from" {
				q.From = t
			} else {
				q.To = t
			}
		}
	}
	go func() {
		result, err := s.agent.ListJobs(q)
		s.post(func() {
			s.jobsLoading = false
			if err != nil {
				s.showMessage(err.Error())
				return
			}
			if more {
				s.jobs = append(s.jobs, result.Items...)
			} else {
				s.jobs = result.Items
			}
			s.cursor = result.Cursor
			s.counts = result.Counts
		})
	}()
}

var nativeStatuses = map[string]string{"Svi": "Svi statusi", "sent": "Poslano", "queued": "U redu čekanja", "sending": "Slanje", "failed": "Neuspješno", "unknown": "Neizvjesno"}

func nativeTime(value any) string {
	switch v := value.(type) {
	case time.Time:
		return v.In(time.Local).Format("02.01.2006. 15:04:05")
	case *time.Time:
		if v == nil {
			return "—"
		}
		return nativeTime(*v)
	}
	raw := textValue(value)
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.In(time.Local).Format("02.01.2006. 15:04:05")
	}
	if raw == "" {
		return "—"
	}
	return raw
}

func nativeStatusBadge(c *ui.Context, status string) {
	bg, fg := ui.RGB(234, 243, 255), ui.RGB(57, 132, 210)
	switch status {
	case "sent":
		bg, fg = ui.RGB(234, 248, 237), ui.RGB(41, 144, 70)
	case "failed":
		bg, fg = ui.RGB(255, 240, 237), ui.RGB(193, 74, 57)
	case "unknown":
		bg, fg = ui.RGB(255, 245, 228), ui.RGB(167, 119, 32)
	}
	studioText(c, nativeStatuses[status]).AlignSelf(ui.Start).FontSize(10).TextColor(fg).Background(bg).PaddingX(7).PaddingY(5).Radius(5)
}

func (s *nativeStudio) jobsView(c *ui.Context) {
	studioHeading(c, "Svaki račun, na jednom mjestu.", "Filtriraj račune prema statusu i vremenu prijema.", func() {
		if studioIconButton(c, "Osvježi poslove", "repeat").Disabled(s.jobsLoading).Clicked() {
			s.loadJobs(false)
		}
	})
	studioCard(c).Padding(20).Children(func() {
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.End).Children(func() {
			ui.Column(c).Width(155).Gap(7).Children(func() {
				studioLabel(c, "Status")
				selected := nativeStatuses[s.status]
				keys := []string{"Svi", "queued", "sending", "sent", "failed", "unknown"}
				names := []string{}
				for _, k := range keys {
					names = append(names, nativeStatuses[k])
				}
				if studioSelect(c, &selected, names).Label(tr(c, "Status poslova")).FillWidth().Height(42).Changed() {
					for _, k := range keys {
						if nativeStatuses[k] == selected {
							s.status = k
						}
					}
				}
			})
			ui.Column(c).Width(0).Grow(1).MinWidth(0).Gap(7).Children(func() {
				studioLabel(c, "Od")
				studioDateTimePicker(c, &s.from, "Vrijeme od", false)
			})
			ui.Column(c).Width(0).Grow(1).MinWidth(0).Gap(7).Children(func() {
				studioLabel(c, "Do")
				studioDateTimePicker(c, &s.to, "Vrijeme do", true)
			})
			ready := !s.jobsLoading && (s.status != s.appliedStatus || s.from != s.appliedFrom || s.to != s.appliedTo)
			if studioButton(c, "Primijeni", ready).Height(42).Disabled(!ready).Clicked() {
				if s.from != "" && s.to != "" && s.from > s.to {
					s.showMessage("Početak raspona mora biti prije kraja.")
					return
				}
				s.appliedStatus, s.appliedFrom, s.appliedTo = s.status, s.from, s.to
				s.jobs = nil
				s.loadJobs(false)
			}
		})
	})
	ui.Row(c).FillWidth().Gap(16).Children(func() {
		for i, label := range []string{"Poslano", "U redu čekanja", "Za provjeru"} {
			value := []int{s.counts["sent"], s.counts["queued"] + s.counts["sending"], s.counts["failed"] + s.counts["unknown"]}[i]
			studioCard(c).Width(0).Grow(1).Gap(10).Children(func() { studioLabel(c, label); studioText(c, fmt.Sprint(value)).FontSize(27).FontWeight(600) })
		}
	})
	studioCard(c).Padding(0).Gap(0).Children(func() {
		if len(s.jobs) == 0 {
			ui.Column(c).FillWidth().Padding(40).Gap(12).AlignItems(ui.Center).Children(func() {
				studioText(c, "Nema poslova za odabrane filtere").FontSize(15).FontWeight(600)
				studioLabel(c, "Promijeni raspon vremena ili status.")
			})
		}
		for _, j := range s.jobs {
			id := textValue(j["id"])
			ui.Row(c).Key(id).FillWidth().PaddingX(24).PaddingY(18).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(34, 34).Radius(9).Background(studioBG).Center().Children(func() { ui.Icon(c, studioIcons["template"]).Size(20, 20).TextColor(studioMuted) })
				ui.Column(c).Width(0).Grow(1).MinWidth(0).Gap(6).Children(func() {
					ui.Text(c, textValue(j["printer_name"])).FontSize(12).FontWeight(550)
					studioText(c, i18n.BosnianPrintTerms(textValue(j["message"]))).FontSize(10).TextColor(studioMuted).FillWidth()
					studioText(c, id+fmt.Sprintf(" · predložak v%v", j["template_version"])).FontSize(10).TextColor(studioMuted)
				})
				ui.Column(c).Gap(7).AlignItems(ui.End).Children(func() {
					nativeStatusBadge(c, textValue(j["status"]))
					studioText(c, nativeTime(j["created_at"])+fmt.Sprintf(" · %v B", j["byte_count"])).FontSize(10).TextColor(studioMuted)
				})
				if studioIconButton(c, "Detalji", "details").Clicked() {
					s.openJob(id)
				}
				if studioIconButton(c, "Ponovi", "repeat").Disabled(j["status"] == "queued" || j["status"] == "sending").Clicked() {
					s.reprint(id)
				}
			})
			ui.Divider(c)
		}
	})
	if s.cursor != "" {
		end := studioText(c, "Učitavanje…").Key("more").Padding(16).FontSize(11).TextColor(studioMuted)
		_, height := c.Size()
		if r := end.Bounds(); r.H > 0 && r.Y < height && r.Y+r.H > 0 {
			s.loadJobs(true)
		}
	}
}

// Release transient preview data; persisted receipts and revisions stay intact.
func (s *nativeStudio) closeModal() {
	s.modal = ""
	s.jobBitmap = nil
	s.job = nil
	s.historyBitmap = nil
	s.historyPreviewLoaded = false
}

func (s *nativeStudio) openJob(id string) {
	job, err := s.agent.JobDetails(id)
	if err != nil {
		s.showMessage(err.Error())
		return
	}
	s.job = job
	s.jobTab = "Detalji"
	s.jobBitmap = nil
	s.modal = "job"
}

func (s *nativeStudio) reprint(id string) {
	s.confirm("Ponovo isprintati račun „"+id+"“?", func() {
		if _, err := s.agent.ReprintJob(id); err != nil {
			s.showMessage(err.Error())
		} else {
			s.showMessage("Kopija je dodana u red printanja.")
			s.loadJobs(false)
		}
	})
}

func (s *nativeStudio) jobView(c *ui.Context) {
	studioText(c, textValue(s.job["id"])).FontSize(11).TextColor(studioMuted)
	studioTabs(c, &s.jobTab, []string{"Detalji", "Payload", "Pregled"})
	_, height := c.Size()
	ui.Scroll(c).FillWidth().Height(min(450, height-300)).Background(studioBG).Radius(10).Padding(16).Children(func() {
		if s.jobTab == "Pregled" {
			if s.jobBitmap == nil {
				result, err := s.agent.JobPreview(textValue(s.job["id"]))
				if err != nil {
					studioText(c, err.Error()).FillWidth()
				} else {
					s.jobBitmap, _ = ui.DecodeBitmap(result.Data)
				}
			}
			if s.jobBitmap != nil {
				ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() { ui.Image(c, s.jobBitmap).Width(360) })
			}
			return
		}
		if s.jobTab == "Payload" {
			b, _ := json.Marshal(s.job["data"])
			studioCode(c, "Payload računa", prettyJSON(b))
			return
		}
		ui.Column(c).FillWidth().Gap(16).Children(func() {
			nativeStatusBadge(c, textValue(s.job["status"]))
			studioText(c, i18n.BosnianPrintTerms(textValue(s.job["message"]))).FillWidth().FontSize(12)
			printer, _ := s.job["printer"].(model.Printer)
			template, _ := s.job["template"].(agent.JobTemplateInfo)
			address := printer.Host + fmt.Sprintf(":%v", printer.Port)
			if printer.Kind == "usb" {
				address = "USB · " + printer.Queue
			}
			fields := [][2]string{{"Primljeno", nativeTime(s.job["created_at"])}, {"Posljednja promjena", nativeTime(s.job["updated_at"])}, {"Slanje započelo", nativeTime(s.job["started_at"])}, {"Slanje završeno", nativeTime(s.job["completed_at"])}, {"Printer", printer.Name}, {"Veza pri slanju", address}, {"Predložak", template.Name + fmt.Sprintf(" · v%v", s.job["template_version"])}, {"Veličina komandi", fmt.Sprintf("%v B", s.job["byte_count"])}}
			if original := textValue(s.job["original_job_id"]); original != "" {
				fields = append(fields, [2]string{"Kopija posla", original})
			}
			for i := 0; i < len(fields); i += 2 {
				ui.Row(c).FillWidth().Gap(16).Children(func() {
					for j := i; j < min(i+2, len(fields)); j++ {
						f := fields[j]
						ui.Column(c).Width(0).Grow(1).Gap(6).Children(func() { studioLabel(c, f[0]); ui.Text(c, f[1]).FontSize(12).FillWidth() })
					}
				})
			}
			b, _ := json.Marshal(s.job["data"])
			studioCode(c, "Payload", prettyJSON(b))
		})
	})
	ui.Row(c).FillWidth().Justify(ui.End).Gap(9).Children(func() {
		if studioButton(c, "Zatvori detalje", false).Clicked() {
			s.closeModal()
		}
		if studioButton(c, "Ponovi", true).Disabled(s.job["can_reprint"] != true).Clicked() {
			s.reprint(textValue(s.job["id"]))
		}
	})
}

func studioCode(c *ui.Context, label, value string) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		studioLabel(c, label)
		studioTextArea(c, &value).ReadOnly(true).Label(tr(c, label) + " " + tr(c, "sadržaj")).FillWidth().Height(220).Font("monospace").FontSize(11).Background(ui.RGB(247, 247, 250))
	})
}

// Localize only the bundled demonstration data. User-owned payloads retain
// their keys and values, regardless of the interface language.
func (s *nativeStudio) apiSampleData(raw json.RawMessage) json.RawMessage {
	if s.language != "en" {
		return raw
	}
	var data, defaults any
	if json.Unmarshal(raw, &data) != nil || json.Unmarshal(s.defaultExampleData, &defaults) != nil || !reflect.DeepEqual(data, defaults) {
		return raw
	}
	var localize func(any) any
	localize = func(v any) any {
		switch x := v.(type) {
		case string:
			return i18n.Translate(s.language, x)
		case map[string]any:
			for k, value := range x {
				x[k] = localize(value)
			}
		case []any:
			for i, value := range x {
				x[i] = localize(value)
			}
		}
		return v
	}
	result, _ := json.Marshal(localize(data))
	return result
}

func (s *nativeStudio) apiExample(includeKey bool) (string, string, string) {
	key := "YOUR_API_KEY"
	if includeKey {
		key = s.config.APIKey
	}
	method, path := "POST", "/v1/print-jobs"
	if s.apiTab == "Status posla" {
		method, path = "GET", "/v1/print-jobs/receipt-2026-0042"
	}
	if s.apiTab == "Status agenta" {
		method, path = "GET", "/v1/health"
	}
	if s.apiTab == "Preuzimanje računa" {
		method, path = "GET", "/v1/print-jobs/receipt-2026-0042/receipt"
		request := fmt.Sprintf("const API_KEY = %q;\n\nconst response = await fetch(%q, {\n  headers: { Authorization: \"Bearer \" + API_KEY }\n});\nif (!response.ok) throw new Error((await response.json()).error);\nconst receipt = await response.blob();\nconsole.log(receipt);", key, s.agent.Origin()+path)
		response := "HTTP 200\nContent-Type: image/png\nContent-Disposition: attachment; filename=receipt-2026-0042.png\n\n[" + i18n.Translate(s.language, "Binarni sadržaj slike računa") + "]"
		return method + " " + path, request, response
	}
	options := map[string]any{"method": method, "headers": map[string]string{"Authorization": "Bearer " + key}}
	printer := model.Printer{}
	for _, p := range s.config.Printers {
		if p.ID == s.config.DefaultPrinter {
			printer = p
		}
	}
	if printer.ID == "" && len(s.config.Printers) > 0 {
		printer = s.config.Printers[0]
	}
	template := model.ReceiptTemplate{}
	for _, t := range s.config.Templates {
		if t.ID == printer.TemplateID {
			template = t
		}
	}
	if template.ID == "" && len(s.config.Templates) > 0 {
		template = s.config.Templates[0]
	}
	if method == "POST" {
		options["headers"].(map[string]string)["Content-Type"] = "application/json"
		payload := map[string]any{"version": 1, "job_id": "receipt-2026-0042", "return_png": true, "data": s.apiSampleData(template.SampleData)}
		if printer.ID != "" {
			payload["printer_id"] = printer.ID
		}
		body, _ := json.Marshal(payload)
		options["body"] = string(body)
	}
	raw, _ := json.MarshalIndent(options, "", "  ")
	request := fmt.Sprintf("const API_KEY = %q;\n\nconst response = await fetch(%q, %s);\nconst result = await response.json();\nif (!response.ok) throw new Error(result.error);\nconsole.log(result);", key, s.agent.Origin()+path, strings.Replace(string(raw), `"Bearer `+key+`"`, `"Bearer " + API_KEY`, 1))
	response := map[string]any{"id": "receipt-2026-0042", "printer_id": printer.ID, "printer_name": printer.Name, "template_version": template.Version, "status": "sent", "message": i18n.Translate(s.language, "Podaci su predati printeru."), "byte_count": 512, "created_at": "2026-11-05T10:30:00+01:00", "updated_at": "2026-11-05T10:30:01+01:00"}
	if method == "POST" {
		width, margin, paper := 640, 32, 80
		var templateData map[string]any
		_ = json.Unmarshal(template.Template, &templateData)
		if templateData["profile"] == "narrow" {
			width, margin, paper = 464, 40, 58
		}
		response["status"] = "queued"
		response["message"] = i18n.Translate(s.language, "Čeka slanje printeru.")
		response["duplicate"] = false
		response["png"] = map[string]any{"content_type": "image/png", "encoding": "base64", "width": width, "height": 756, "dpi": 203.2, "paper_width_mm": paper, "margin_left": margin, "margin_right": margin, "scale": 1, "data": "iVBORw0KGgo… (" + i18n.Translate(s.language, "skraćeni primjer") + ")"}
	}
	if s.apiTab == "Status agenta" {
		response = map[string]any{"status": "ready", "version": 1, "app_version": resources.AppVersion}
	}
	raw, _ = json.MarshalIndent(response, "", "  ")
	return method + " " + path, request, string(raw)
}

func (s *nativeStudio) apiView(c *ui.Context) {
	studioHeading(c, "Poveži svoju aplikaciju.", "Odaberi API rutu, kopiraj primjer i poveži svoju aplikaciju.", nil)
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Gap(20).Children(func() {
			studioCard(c).Children(func() {
				studioText(c, "API pristup").FontSize(18).FontWeight(600)
				studioLabel(c, "Adresa agenta")
				address := s.agent.Origin()
				ui.TextInput(c, &address).ReadOnly(true).FillWidth().Height(40).Label(tr(c, "Adresa agenta"))
				studioLabel(c, "API ključ")
				key := s.config.APIKey
				input := ui.TextInput(c, &key).ReadOnly(true).FillWidth().Height(40).Label(tr(c, "API ključ"))
				if !s.showKey {
					input.Password()
				}
				ui.Row(c).Gap(8).Children(func() {
					if studioButton(c, "Kopiraj ključ", false).Clicked() {
						mygo.Clipboard.WriteText(s.config.APIKey)
						s.showMessage("API ključ je kopiran.")
					}
					label := "Prikaži"
					if s.showKey {
						label = "Sakrij"
					}
					if studioButton(c, label, false).Clicked() {
						s.showKey = !s.showKey
					}
					if studioButton(c, "Novi ključ", false).Clicked() {
						s.confirm("Generisati novi ključ? Povezane aplikacije će trebati novi ključ za pristup.", func() {
							if _, err := s.agent.RotateAPIKey(); err != nil {
								s.showMessage(err.Error())
							} else {
								s.reload()
								s.showMessage("Novi ključ je spreman.")
							}
						})
					}
				})
				studioText(c, "Svaka ruta zahtijeva Authorization: Bearer zaglavlje. Novi ključ odmah poništava prethodni.").FontSize(11).TextColor(studioMuted).FillWidth()
			})
			studioCard(c).Children(func() {
				studioTabs(c, &s.apiTab, []string{"Slanje računa", "Preuzimanje računa", "Status posla", "Status agenta"})
				endpoint, request, response := s.apiExample(false)
				ui.Text(c, endpoint).FillWidth().Padding(12).Radius(8).Background(studioBG).Font("monospace").FontSize(12)
				if s.apiTab == "Slanje računa" {
					studioText(c, "Polja body-a").FontSize(17).FontWeight(600)
					studioLabel(c, "Ova polja su izvan objekta data.")
					fields := [][2]string{
						{"version · integer · obavezno", "Verzija zahtjeva. Jedina podržana vrijednost je 1."},
						{"job_id · string · obavezno", "Jedinstveni ID: 1–100 znakova (A–Z, a–z, 0–9, - i _). Isti ID i podaci vraćaju postojeći posao bez novog printanja. Izmijenjeni podaci za isti ID vraćaju HTTP 409."},
						{"printer_id · string · opcionalno", "ID printera na ovom agentu. Prazno ili izostavljeno koristi podrazumijevani printer i njegov predložak."},
						{"return_png · boolean · opcionalno", "false (podrazumijevano): novi posao vraća HTTP 202 bez slike. true: HTTP 200 s PNG slikom u base64 formatu. Postojeći posao vraća HTTP 200. Ovo polje ne isključuje printanje."},
					}
					for _, f := range fields {
						ui.Column(c).FillWidth().Gap(6).Children(func() {
							studioText(c, f[0]).FontSize(12).FontWeight(550)
							studioText(c, f[1]).FontSize(12).TextColor(studioMuted).FillWidth()
						})
					}
					for _, p := range s.config.Printers {
						ui.Text(c, p.ID+" — "+p.Name).FontSize(11).TextColor(studioMuted)
					}
				} else if s.apiTab == "Status posla" {
					studioText(c, "Statusi: queued, sending, sent, failed ili unknown. Poslano znači da su komande predate vezi ili sistemskom redu; fizičko printanje nije potvrđeno. Ova ruta ne vraća PNG ni payload.").FontSize(12).TextColor(studioMuted).FillWidth()
				} else if s.apiTab == "Preuzimanje računa" {
					studioText(c, "Vraća sliku sačuvanog računa kao image/png, bez Base64 i bez novog printanja. Ako slika još ne postoji, generiše se iz originalnih komandi. Nepostojeći posao vraća 404; greška generisanja 422; greška baze 500.").FontSize(12).TextColor(studioMuted).FillWidth()
				} else {
					studioText(c, "Provjerava dostupnost agenta. Status ready ne provjerava vezu s printerom niti stanje papira.").FontSize(12).TextColor(studioMuted).FillWidth()
				}
				if studioButton(c, "Kopiraj JS primjer", false).Clicked() {
					_, example, _ := s.apiExample(true)
					mygo.Clipboard.WriteText(example)
					s.showMessage("Primjer s API ključem je kopiran.")
				}
				studioCode(c, "Primjer requesta", request)
				studioCode(c, "Primjer responsa · HTTP 200", response)
				if s.apiTab == "Preuzimanje računa" {
					studioText(c, "Uspješan odgovor je binarna slika; pročitaj ga pomoću response.blob() ili sačuvaj kao fajl. Greške su JSON. Kopirani primjer uključuje tvoj API ključ.").FontSize(11).TextColor(studioMuted).FillWidth()
				} else {
					studioText(c, "Ilustrativni response: broj bajtova i dimenzije PNG-a zavise od računa. Base64 je skraćen. Kopirani primjer uključuje tvoj API ključ.").FontSize(11).TextColor(studioMuted).FillWidth()
				}
			})
		})
	})
}
