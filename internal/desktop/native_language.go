//go:build desktop && (darwin || windows)

package desktop

import (
	"github.com/egoist/mygo/ui"

	"receiptstudio.local/agent/internal/i18n"
	"receiptstudio.local/agent/internal/model"
)

func nativeLanguage(c *ui.Context) *string {
	return ui.Local(c.Root(), "interface-language", func() string { return "bs" })
}

func tr(c *ui.Context, text string) string { return i18n.Translate(*nativeLanguage(c), text) }

func studioText(c *ui.Context, text string) ui.Element { return ui.Text(c, tr(c, text)) }

// Translate fixed choices while retaining stable values in the model.
func studioSelect(c *ui.Context, value *string, options []string) ui.Element {
	labels := make([]string, len(options))
	selected := tr(c, *value)
	for i, option := range options {
		labels[i] = tr(c, option)
	}
	input := ui.Select(c, &selected, labels)
	if input.Changed() {
		for i, label := range labels {
			if label == selected {
				*value = options[i]
				break
			}
		}
	}
	return input
}

func (s *nativeStudio) changeLanguage(language string) {
	if language == s.language {
		return
	}
	if err := s.agent.SavePreferences(model.Preferences{Language: language}); err != nil {
		s.showMessage(err.Error())
		return
	}
	s.language = language
	if s.editor != nil {
		s.editor.language = language
	}
	if s.onLanguageChanged != nil {
		s.onLanguageChanged(language)
	}
}

func (s *nativeStudio) languageToggle(c *ui.Context) {
	ui.Row(c).Key("language-toggle").Height(28).Gap(2).Padding(2).Radius(7).Background(studioBG).Children(func() {
		for _, language := range []string{"bs", "en"} {
			label, face := "Bosanski", "BS"
			if language == "en" {
				label, face = "English", "EN"
			}
			b := ui.ButtonBase(c.Key(language)).Label(label).Tooltip(label).Width(29).Height(24).Radius(5)
			color := studioMuted
			if s.language == language {
				b.Background(studioWhite)
				color = studioInk
			}
			b.Children(func() { ui.Text(c, face).FontSize(10).FontWeight(550).TextColor(color) })
			if b.Clicked() {
				s.changeLanguage(language)
			}
		}
	})
}
