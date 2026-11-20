//go:build desktop && (darwin || windows)

package desktop

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	resources "receiptstudio.local/agent"
)

func demoUpdateVersion() string {
	base, _, _ := strings.Cut(resources.AppVersion, "+")
	base, _, _ = strings.Cut(base, "-")
	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return resources.AppVersion + "-demo"
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return resources.AppVersion + "-demo"
	}
	return fmt.Sprintf("%s.%s.%d", parts[0], parts[1], patch+1)
}

// Opt-in UI preview only. It never contacts an update service, installs a
// package, changes the running version or writes update preferences.
func (s *nativeStudio) updateDemoView(c *ui.Context) {
	if !s.updateDemo || !s.updateDemoOpen {
		return
	}
	studioModal(c, "update-demo", "SIMULACIJA NADOGRADNJE", tr(c, "Nova verzija je dostupna"), 500, &s.updateDemoOpen, func() {
		studioText(c, "Receipt Studio "+demoUpdateVersion()).FontSize(21).FontWeight(600)
		studioText(c, fmt.Sprintf("Trenutna verzija: %s", resources.AppVersion)).FontSize(12).TextColor(studioMuted)
		ui.Column(c).FillWidth().Padding(16).Gap(10).Radius(10).Background(studioBG).Children(func() {
			studioText(c, "Primjer novosti").FontSize(13).FontWeight(600)
			for _, text := range []string{
				"Izbor datuma i vremena u filterima poslova.",
				"Jasnije obavijesti na vrhu aplikacije.",
				"Dorade izgleda editora predložaka.",
			} {
				studioText(c, "• "+tr(c, text)).FillWidth().FontSize(12)
			}
		})
		studioText(c, fmt.Sprintf("Ovo je lokalni pregled obavijesti o nadogradnji. Verzija %s nije preuzeta niti instalirana.", demoUpdateVersion())).FillWidth().FontSize(12).TextColor(studioMuted)
		ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
			if studioButton(c, "Kasnije", false).Clicked() {
				s.updateDemoOpen = false
			}
		})
	})
}
