//go:build desktop && (darwin || windows)

package desktop

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

const jobFilterTimeFormat = "2006-01-02 15:04"

type nativeDateTimeState struct {
	open        bool
	date, month time.Time
}

// The empty string means an unbounded filter. Draft selection stays inside the
// popover until confirmed; dismissing it leaves the filter unchanged.
func studioDateTimePicker(c *ui.Context, value *string, label string, end bool) {
	wrap := ui.Column(c).FillWidth()
	state := ui.Local(wrap, "date-time", func() nativeDateTimeState { return nativeDateTimeState{} })
	wrap.Children(func() {
		caption := tr(c, "Odaberi datum i vrijeme")
		if date, err := time.ParseInLocation(jobFilterTimeFormat, *value, time.Local); err == nil {
			caption = date.Format("02.01.2006. 15:04")
		}
		button := ui.ButtonBase(c).Label(tr(c, label)).FillWidth().Height(42).PaddingX(10).Gap(8).Radius(8).Border(1, studioLine).Background(studioWhite)
		button.Children(func() {
			ui.Text(c, caption).FontSize(11).Grow(1).SingleLine()
			ui.Icon(c, studioIcons["calendar"]).Size(16, 16).TextColor(studioMuted)
		})
		if button.Clicked() {
			state.open = !state.open
			state.date, _ = time.ParseInLocation(jobFilterTimeFormat, *value, time.Local)
			if state.date.IsZero() {
				now := time.Now()
				hour, minute := 0, 0
				if end {
					hour, minute = 23, 59
				}
				state.date = time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.Local)
			}
			state.month = time.Date(state.date.Year(), state.date.Month(), 1, 0, 0, 0, 0, time.Local)
		}
		ui.PopoverBase(c, button, &state.open, func(panel ui.Element) {
			// When neither side has room, align the panel with the field's
			// center so its calendar and confirmation buttons stay reachable.
			_, height := c.Size()
			bounds := button.Bounds()
			if height-bounds.Y-bounds.H < 400 && bounds.Y < 400 {
				panel.AttachTo(button, ui.AnchorLeft, ui.AnchorLeft)
			}
			panel.Width(292).Padding(16).Gap(12).Radius(12).Background(studioWhite).Border(1, studioLine).Shadow(0, 6, 24, 0, ui.RGBA(0, 0, 0, .14)).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					if studioButton(c, "‹", false).Label(tr(c, "Prethodni mjesec")).Width(30).PaddingX(0).Clicked() {
						state.month = state.month.AddDate(0, -1, 0)
					}
					months := []string{"Januar", "Februar", "Mart", "April", "Maj", "Juni", "Juli", "August", "Septembar", "Oktobar", "Novembar", "Decembar"}
					ui.Text(c, fmt.Sprintf("%s %d", tr(c, months[state.month.Month()-1]), state.month.Year())).Grow(1).TextAlign(ui.Center).FontSize(12).FontWeight(600)
					if studioButton(c, "›", false).Label(tr(c, "Sljedeći mjesec")).Width(30).PaddingX(0).Clicked() {
						state.month = state.month.AddDate(0, 1, 0)
					}
				})
				ui.Row(c).FillWidth().Children(func() {
					for _, day := range []string{"Pon", "Uto", "Sri", "Čet", "Pet", "Sub", "Ned"} {
						studioText(c, day).Width(0).Grow(1).TextAlign(ui.Center).FontSize(10).TextColor(studioMuted)
					}
				})
				ui.Column(c).FillWidth().Gap(2).Children(func() {
					first := state.month.AddDate(0, 0, -(int(state.month.Weekday())+6)%7)
					for week := 0; week < 6; week++ {
						ui.Row(c).Key(week).FillWidth().Children(func() {
							for weekday := 0; weekday < 7; weekday++ {
								day := first.AddDate(0, 0, week*7+weekday)
								selected := day.Format("2006-01-02") == state.date.Format("2006-01-02")
								b := studioButton(c.Key(weekday), fmt.Sprint(day.Day()), selected).Label(day.Format("2006-01-02")).Width(0).Grow(1).Height(28).PaddingX(0).Border(0, studioLine)
								if !selected && day.Month() != state.month.Month() {
									b.Opacity(.45)
								}
								if b.Clicked() {
									state.date = time.Date(day.Year(), day.Month(), day.Day(), state.date.Hour(), state.date.Minute(), 0, 0, time.Local)
								}
							}
						})
					}
				})
				ui.Row(c).FillWidth().Gap(12).Children(func() {
					hour, minute := float64(state.date.Hour()), float64(state.date.Minute())
					for i, field := range []struct {
						label string
						value *float64
						max   float64
					}{{"Sati", &hour, 23}, {"Minute", &minute, 59}} {
						ui.Column(c).Key(i).Width(0).Grow(1).Gap(4).Children(func() {
							studioLabel(c, field.label)
							studioNumber(c, field.value, 0, field.max, 1).Label(tr(c, "Odabrano vrijeme") + " · " + tr(c, field.label))
						})
					}
					state.date = time.Date(state.date.Year(), state.date.Month(), state.date.Day(), int(hour), int(minute), 0, 0, time.Local)
				})
				ui.Row(c).FillWidth().Justify(ui.SpaceBetween).Children(func() {
					if studioButton(c, "Očisti", false).Clicked() {
						*value = ""
						state.open = false
					}
					if studioButton(c, "Odaberi", true).Clicked() {
						*value = state.date.Format(jobFilterTimeFormat)
						state.open = false
					}
				})
			})
		})
	})
}
