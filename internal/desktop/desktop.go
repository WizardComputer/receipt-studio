//go:build desktop && (darwin || windows)

package desktop

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	resources "receiptstudio.local/agent"
	"receiptstudio.local/agent/internal/agent"
	"receiptstudio.local/agent/internal/i18n"
)

var nativeTrayIcon, _ = resources.Assets.ReadFile("packaging/assets/tray.png")

const DesktopBuild = true

var DesktopActive bool

func ReportDesktopError(message string) {
	if DesktopActive {
		desktopAlert(message)
	}
}

func RunDesktop(ctx context.Context, stop context.CancelFunc, origin, dir string, args []string, setup bool, agent *agent.Agent) {
	mygo.App.OnWindowAllClosed(func() {})
	studio := newNativeStudio(agent)
	studio.updateDemo = os.Getenv("RECEIPT_SIMULATE_UPDATE") == "1"
	studio.updateDemoOpen = studio.updateDemo
	mygo.App.WhenReady(func() {
		// Receipt Studio uses a light palette; keep toolkit text and controls in
		// the same appearance even when the operating system uses dark mode.
		mygo.Theme.SetSource(mygo.ThemeLight)
		open := func() {
			if studio.window == nil {
				// Use logical window dimensions, including the title bar. Keep the
				// desktop layout spacious without exceeding a smaller work area.
				width, height := 1400, 760
				workArea := mygo.Screen.PrimaryDisplay().WorkArea
				if workArea.Width > 0 {
					width = min(width, workArea.Width)
				}
				if workArea.Height > 0 {
					height = min(height, workArea.Height)
				}
				studio.window = mygo.NewWindow(mygo.WindowOptions{
					Title: "Receipt Studio", Width: width, Height: height,
					MinWidth: width, MinHeight: height, Content: ui.View(studio.view),
				})
				studio.window.OnClose(func(event *mygo.CloseEvent) {
					if !studio.quitting && ctx.Err() == nil {
						event.PreventDefault()
						studio.window.Hide()
					}
				})
				studio.window.OnClosed(func() { studio.window = nil })
			} else {
				studio.window.Show()
				studio.window.Focus()
			}
		}
		menu := mygo.NewMenu([]*mygo.MenuItem{{ID: "open", Label: i18n.Translate(studio.language, "Otvori Receipt Studio"), Click: func(*mygo.MenuItem, *mygo.Window) { open() }}, {ID: "autostart", Label: i18n.Translate(studio.language, "Pokreni pri prijavi"), Click: func(*mygo.MenuItem, *mygo.Window) {
			if err := setAutostart(!autostartEnabled(), args); err != nil {
				studio.showMessage(err.Error())
			}
		}}, mygo.Separator(), {ID: "quit", Label: i18n.Translate(studio.language, "Ugasi agent"), Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }}})
		studio.onLanguageChanged = func(language string) {
			for id, label := range map[string]string{"open": "Otvori Receipt Studio", "autostart": "Pokreni pri prijavi", "quit": "Ugasi agent"} {
				menu.ItemByID(id).SetLabel(i18n.Translate(language, label))
			}
		}
		icon := nativeTrayIcon
		if _, err := mygo.NewTray(mygo.TrayOptions{ID: "receipt-studio", Icon: icon, IconIsTemplate: true, ToolTip: "Receipt Studio", Menu: menu}); err != nil {
			log.Print(err)
		}
		if !agent.DesktopInitialized() && setup {
			if err := setAutostart(true, args); err == nil {
				if err := agent.MarkDesktopInitialized(); err != nil {
					log.Print(err)
				}
			} else {
				log.Print(err)
			}
		}
		open()
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					mygo.App.Quit()
					return
				case <-ticker.C:
					mygo.RunOnMain(func() {
						if studio.page == "Poslovi printanja" && studio.modal == "" && len(studio.jobs) <= 100 {
							studio.loadJobs(false)
						}
					})
				}
			}
		}()
	})
	mygo.App.OnBeforeQuit(func(event *mygo.QuitEvent) {
		if ctx.Err() == nil && studio.dirty() && !studio.quitting {
			event.PreventDefault()
			if studio.window != nil {
				studio.window.Show()
				studio.window.Invalidate()
			}
			studio.confirm("Odbaciti nesačuvane izmjene i ugasiti agent?", func() { studio.quitting = true; mygo.App.Quit() })
			return
		}
		studio.quitting = true
		stop()
	})
	if err := mygo.App.Run(); err != nil {
		log.Print(err)
		stop()
	}
}
