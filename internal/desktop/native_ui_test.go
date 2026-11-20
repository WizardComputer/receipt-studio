//go:build desktop && (darwin || windows)

package desktop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/receipt"
)

func TestNativeElementNameAndProfile(t *testing.T) {
	enc, _ := receipt.NewEncoder()
	raw, data, _ := enc.Defaults()
	e, _ := newNativeEditor(enc, raw, data)
	e.doc.Selected = "brand"
	tt := ui.NewTester(e.view, 1500, 1000)
	before := append([]byte(nil), e.bytes...)
	if _, ok := tt.Find("Protokol printera"); ok {
		t.Fatal("printer profile should start collapsed")
	}
	if err := tt.Click("Profil printera"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("Protokol printera"); !ok {
		t.Fatal("printer profile did not expand")
	}
	if err := tt.Click("Profil printera"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Uredi naziv elementa"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Zaglavlje računa")
	tt.Frame()
	if e.doc.ElementName(e.doc.Element("brand")) != "Zaglavlje računa" {
		t.Fatalf("name edit was not saved: %q", e.doc.ElementName(e.doc.Element("brand")))
	}
	d, err := receipt.OpenNativeDocument(e.doc.JSON(), data)
	if err != nil || d.ElementName(d.Element("brand")) != "Zaglavlje računa" {
		t.Fatal("name lost after reopening", err)
	}
	_, after, err := enc.NativeCompile(d)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("renaming an element changed printer commands", err)
	}
	e.doc.Undo()
	if e.doc.ElementName(e.doc.Element("brand")) == "Zaglavlje računa" {
		t.Fatal("rename cannot be undone")
	}
}

func TestNativeLanguageSwitchPreservesEditorAndUserNames(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 9)
	p.Name = "Predlošci"
	if _, err := a.SavePrinter(p, false); err != nil {
		t.Fatal(err)
	}
	s := newNativeStudio(a)
	tt := ui.NewTester(s.view, 1000, 650)
	if err := tt.Click("English"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, label := range []string{"Printers", "Templates", "Print jobs", "API integration", "Add printer", "Predlošci"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatal("missing translated label or original user name", label)
		}
	}
	if dir := os.Getenv("RECEIPT_NATIVE_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for _, page := range []string{"Printeri", "Predlošci", "Poslovi printanja", "API povezivanje"} {
			s.page = page
			tt.Frame()
			f, err := os.Create(filepath.Join(dir, page+"-English-1000.png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	s.openEditor(s.config.Templates[0])
	s.editor.doc.Selected = "brand"
	s.editor.doc.Set("brand", "text", "Nesačuvani naziv firme")
	tt.Frame()
	before := string(s.editor.doc.JSON())
	commands := append([]byte(nil), s.editor.bytes...)
	if _, ok := tt.Find("Save template"); !ok {
		t.Fatal("editor not translated")
	}
	if err := tt.Click("Printer profile"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("Cut paper after receipt"); !ok {
		t.Fatal("profile not translated")
	}
	if err := tt.Click("Bosanski"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("Sačuvaj predložak"); !ok {
		t.Fatal("Bosnian not restored")
	}
	if string(s.editor.doc.JSON()) != before || !bytes.Equal(commands, s.editor.bytes) || !s.dirty() {
		t.Fatal("switch lost or changed unsaved receipt")
	}
	if err := tt.Click("English"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if dir := os.Getenv("RECEIPT_NATIVE_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, "Editor-English-1000.png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tt.Click("Close editor"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("Discard unsaved template changes?"); !ok {
		t.Fatal("confirmation not translated")
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.editor == nil || string(s.editor.doc.JSON()) != before {
		t.Fatal("translated cancel lost changes")
	}
	if got := newNativeStudio(a).language; got != "en" {
		t.Fatal("new window did not load preferred language", got)
	}
}

func TestNativeTemplateHistoryPreviewAndRestore(t *testing.T) {
	a := testAgent(t)
	original := a.ConfigSnapshot().Templates[0]
	entry := original
	var sample map[string]any
	json.Unmarshal(entry.SampleData, &sample)
	sample["shop"] = "Izmijenjena radnja"
	entry.SampleData, _ = json.Marshal(sample)
	if _, err := a.SaveTemplate(entry, false); err != nil {
		t.Fatal(err)
	}
	s := newNativeStudio(a)
	s.page = "Predlošci"
	tt := ui.NewTester(s.view, 1000, 650)
	if err := tt.Click("Historija verzija"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.modal != "history" || s.historyVersion.Version != 2 || s.historyBitmap == nil {
		t.Fatal("history preview did not open", s.historyError)
	}
	if err := tt.Click("Verzija 1"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.historyVersion.Version != 1 || s.historyBitmap == nil || model.CompactJSON(s.historyVersion.SampleData) != model.CompactJSON(original.SampleData) {
		t.Fatal("historical sample/preview missing", s.historyError)
	}
	if dir := os.Getenv("RECEIPT_NATIVE_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, "Template-history-1000.png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tt.Click("Podaci"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Vrati ovu verziju"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.confirmAction == nil || a.ConfigSnapshot().Templates[0].Version != 2 {
		t.Fatal("restore skipped confirmation")
	}
	if err := tt.Click("Potvrdi"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.ConfigSnapshot().Templates[0].Version != 3 || s.historyVersion.Version != 3 || len(s.history.Versions) != 3 {
		t.Fatal("restore did not refresh history")
	}
	if countTestJobs(t, a) != 0 {
		t.Fatal("preview printed a receipt")
	}
	for _, closeLabel := range []string{"Zatvori historiju", "Zatvori"} {
		if err := tt.Click(closeLabel); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if s.modal != "" || s.historyBitmap != nil || s.historyPreviewLoaded {
			t.Fatal("closed history retained preview")
		}
		s.openTemplateHistory(a.ConfigSnapshot().Templates[0])
		tt.Frame()
		if s.historyBitmap == nil || len(s.history.Versions) != 3 {
			t.Fatal("reopening history lost revisions or preview")
		}
	}
}

func TestNativeCanvasDragDropAndBackspace(t *testing.T) {
	enc, _ := receipt.NewEncoder()
	raw, data, _ := enc.Defaults()
	editor, err := newNativeEditor(enc, raw, data)
	if err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(editor.view, 1500, 1200)
	before := append([]byte(nil), editor.bytes...)
	from, ok := tt.Find("Element brand")
	if !ok {
		t.Fatal("brand hit region missing")
	}
	to, ok := tt.Find("Element items")
	if !ok {
		t.Fatal("table hit region missing")
	}
	tt.Press(from.X+10, from.Y+10)
	tt.Move(to.X+15, to.Y+15)
	tt.Release(to.X+15, to.Y+15)
	tt.Frame()
	if editor.doc.Elements()[3].(map[string]any)["id"] != "brand" {
		t.Fatal("pointer drop did not reorder receipt")
	}
	if bytes.Equal(before, editor.bytes) {
		t.Fatal("moving changed no commands")
	}
	editor.doc.Undo()
	tt.Frame()
	if !bytes.Equal(before, editor.bytes) {
		t.Fatal("undo did not recover original bytes")
	}
	if err := tt.Click("Element brand"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyBackspace)
	tt.Frame()
	if editor.doc.Element("brand") != nil {
		t.Fatal("backspace did not remove selection")
	}
}

func TestNativeDropIndicatorMatchesInsertionAndClears(t *testing.T) {
	enc, _ := receipt.NewEncoder()
	raw, data, _ := enc.Defaults()
	e, _ := newNativeEditor(enc, raw, data)
	tt := ui.NewTester(e.view, 1500, 1200)
	before := string(e.doc.JSON())
	from, _ := tt.Find("Element brand")
	to, _ := tt.Find("Element items")
	tt.Press(from.X+10, from.Y+10)
	tt.Move(to.X+15, to.Y+15)
	tt.Frame()
	marker, ok := tt.Find("Mjesto umetanja")
	if !ok || math.Abs(float64(marker.Y-(to.Y-1))) > 1 {
		t.Fatalf("missing/misplaced insertion line: %+v, target %+v", marker, to)
	}
	if string(e.doc.JSON()) != before {
		t.Fatal("hover changed the document")
	}
	tt.Release(to.X+15, to.Y+15)
	tt.Frame()
	if _, ok := tt.Find("Mjesto umetanja"); ok {
		t.Fatal("line remained after drop")
	}
	items := e.doc.Elements()
	for i, item := range items {
		if item.(map[string]any)["id"] == "items" && (i == 0 || items[i-1].(map[string]any)["id"] != "brand") {
			t.Fatal("drop did not use the indicated boundary")
		}
	}
	// New elements use the same marker at the end; Escape must clear it.
	palette, _ := tt.Find("Tekst")
	end, _ := tt.Find("Kraj računa")
	tt.Press(palette.X+5, palette.Y+5)
	tt.Move(end.X+10, end.Y+10)
	tt.Frame()
	marker, ok = tt.Find("Mjesto umetanja")
	if !ok || math.Abs(float64(marker.Y-(end.Y-1))) > 1 {
		t.Fatal("end insertion line missing")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Release(end.X+10, end.Y+10)
	tt.Frame()
	if _, ok := tt.Find("Mjesto umetanja"); ok || len(e.doc.Elements()) != len(items) {
		t.Fatal("cancel retained marker or inserted a new element")
	}
	// The sidebar marks exactly the same before-target position.
	from, _ = tt.Find("Sloj brand")
	to, _ = tt.Find("Sloj thanks")
	tt.Press(from.X+5, from.Y+5)
	tt.Move(to.X+8, to.Y+25)
	tt.Frame()
	r, g, b, _ := tt.Image().At(int(to.X+to.W/2), int(to.Y+1)).RGBA()
	if r != 0 || g != 122*257 || b != 255*257 {
		t.Fatalf("sidebar insertion line missing: %d %d %d", r, g, b)
	}
	tt.Release(to.X+8, to.Y+25)
}

func TestNativeResizeUsesVectorPreviewAndOneUndo(t *testing.T) {
	enc, _ := receipt.NewEncoder()
	raw, data, _ := enc.Defaults()
	e, _ := newNativeEditor(enc, raw, data)
	e.doc.Selected = "number"
	tt := ui.NewTester(e.view, 1000, 650)
	r, ok := tt.Find("Promijeni širinu number")
	if !ok {
		t.Fatal("resize handle missing")
	}
	before := append([]byte(nil), e.bytes...)
	tt.Press(r.X+4, r.Y+8)
	start := time.Now()
	for i := 1; i <= 8; i++ {
		tt.Move(r.X+4-float32(i)*7, r.Y+8)
	}
	t.Logf("eight resize moves: %s", time.Since(start))
	if e.preview == nil {
		t.Error("interactive vector preview missing")
	}
	// Moving less than a character cell must reuse the compiled SVG.
	cached := e.preview
	tt.Move(r.X+4-56-.1, r.Y+8)
	if e.preview != cached {
		t.Error("sub-column motion regenerated preview")
	}

	width := numberValue(e.doc.Element("number")["width"])
	if width != math.Round(width) {
		t.Errorf("resize width has floating-point noise: %.14f", width)
	}
	tt.Release(r.X+4-56, r.Y+8)
	tt.Frame()
	if bytes.Equal(e.bytes, before) {
		t.Error("resize did not change final commands")
	}

	e.doc.Undo()
	if numberValue(e.doc.Element("number")["width"]) != 100 {
		t.Error("one resize must be one undo operation")
	}
	tt.Frame()
	if !bytes.Equal(e.bytes, before) {
		t.Error("undo did not recover exact commands")
	}

}

func TestNativeQuickFormattingAndNumberArrows(t *testing.T) {
	enc, _ := receipt.NewEncoder()
	raw, data, _ := enc.Defaults()
	e, _ := newNativeEditor(enc, raw, data)
	e.doc.Selected = "number"
	tt := ui.NewTester(e.view, 1500, 1000)
	for _, label := range []string{"Poravnanje · Desno", "Veličina fonta · 2×", "Podebljano", "Podvučeno"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	el := e.doc.Element("number")
	if el["align"] != "right" || numberValue(el["scale"]) != 2 || el["bold"] != true || el["underline"] != true {
		t.Fatal("format buttons did not update receipt")
	}
	for _, field := range []struct{ label, key string }{{"Razmak ispod (dots)", "gap"}, {"X (%)", "x"}, {"Širina (%)", "width"}} {
		r, ok := tt.Find(field.label)
		if !ok {
			t.Fatal(field.label)
		}
		tt.Press(r.X+20, r.Y+r.H+20)
		tt.Release(r.X+20, r.Y+r.H+20)
		before := numberValue(el[field.key])
		tt.Key(0, ui.KeyDown)
		tt.Frame()
		want := math.Max(0, before-1)
		if field.key == "width" {
			want = math.Max(10, want)
		}
		if numberValue(el[field.key]) != want {
			t.Fatalf("%s down: %v, want %v", field.key, el[field.key], want)
		}
		tt.Key(0, ui.KeyUp)
		tt.Frame()
		if numberValue(el[field.key]) != want+1 {
			t.Fatalf("%s up: %v", field.key, el[field.key])
		}
	}
}

func TestNativeEditorToolbarMatchesWebRegions(t *testing.T) {
	enc, _ := receipt.NewEncoder()
	raw, data, _ := enc.Defaults()
	e, _ := newNativeEditor(enc, raw, data)
	tt := ui.NewTester(e.view, 1500, 1000)
	for _, label := range []string{"Tekst", "Tabela", "QR", "Slika"} {
		r, ok := tt.Find(label)
		if !ok || r.X < 0 || r.X+r.W > 210 || r.Y < 65 || r.Y > 160 {
			t.Fatalf("palette %s outside web sidebar: %+v", label, r)
		}
	}
	for _, label := range []string{"Dizajn", "Podaci", "Komande", "Zoom"} {
		r, ok := tt.Find(label)
		if !ok || r.X < 210 || r.X+r.W > 1240 || r.Y < 65 || r.Y+r.H > 125 {
			t.Fatalf("toolbar %s outside web canvas header: %+v", label, r)
		}
	}
}

func TestNativeTableAlignmentButtonsAndZoomArrows(t *testing.T) {
	enc, _ := receipt.NewEncoder()
	raw, data, _ := enc.Defaults()
	e, _ := newNativeEditor(enc, raw, data)
	e.doc.Selected = "items"
	tt := ui.NewTester(e.view, 1500, 1600)
	if err := tt.Click("Kolona 1 poravnanje · Desno"); err != nil {
		t.Fatal(err)
	}
	columns := e.doc.Element("items")["columns"].([]any)
	if columns[0].(map[string]any)["align"] != "right" {
		t.Fatal("column alignment unchanged")
	}
	if err := tt.Click("Zoom"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if e.zoom != .9 {
		t.Fatalf("zoom up: %v", e.zoom)
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if e.zoom != .8 {
		t.Fatalf("zoom down: %v", e.zoom)
	}
}

func TestNativeReferenceEditorScreenshot(t *testing.T) {
	dir, path := os.Getenv("RECEIPT_NATIVE_SCREENSHOTS"), os.Getenv("RECEIPT_NATIVE_CONFIG")
	if dir == "" || path == "" {
		t.Skip("optional reference screenshot")
	}
	config := readNativeTestConfig(t, path)
	enc, _ := receipt.NewEncoder()
	entry := config.Templates[0]
	e, err := newNativeEditor(enc, entry.Template, entry.SampleData)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.doc.Elements()) > 0 {
		e.doc.Selected = textValue(e.doc.Elements()[0].(map[string]any)["id"])
	}
	tt := ui.NewTester(e.view, 1500, 1000)
	f, err := os.Create(filepath.Join(dir, "Editor-reference.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestNativePaletteDropsNewText(t *testing.T) {
	enc, _ := receipt.NewEncoder()
	raw, data, _ := enc.Defaults()
	e, _ := newNativeEditor(enc, raw, data)
	tt := ui.NewTester(e.view, 1500, 1200)
	from, _ := tt.Find("Tekst")
	to, _ := tt.Find("Element address")
	tt.Press(from.X+5, from.Y+5)
	tt.Move(to.X+20, to.Y+10)
	tt.Release(to.X+20, to.Y+10)
	tt.Frame()
	if len(e.doc.Elements()) != 8 {
		t.Fatal("palette drop did not add text")
	}
	if e.doc.Elements()[2].(map[string]any)["text"] != "Novi tekst" {
		t.Fatal("palette drop position")
	}
}

func TestNativePrintRequiresConfirmation(t *testing.T) {
	a := testAgent(t)
	addTestPrinter(t, a, "127.0.0.1", 9)
	s := newNativeStudio(a)
	tt := ui.NewTester(s.view, 1280, 850)
	if err := tt.Click("Probni račun"); err != nil {
		t.Fatal(err)
	}
	if countTestJobs(t, a) != 0 || s.confirmAction == nil {
		t.Fatal("printed before confirmation")
	}
	if err := tt.Click("Odustani"); err != nil {
		t.Fatal(err)
	}
	if countTestJobs(t, a) != 0 {
		t.Fatal("cancel printed")
	}
}

func TestNativeScreens(t *testing.T) {
	dir := os.Getenv("RECEIPT_NATIVE_SCREENSHOTS")
	if dir == "" {
		t.Skip("optional native render artifacts")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 9)
	job, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "racun-2026-0042", Data: sampleFor(a, p)})
	if err != nil {
		t.Fatal(err)
	}
	editTestJob(t, a, job.ID, func(j *model.Job) { j.Status = "sent" })
	editTestJob(t, a, job.ID, func(j *model.Job) { j.Message = "Podaci su predati printeru." })
	job.Status = "sent"
	job.Message = "Podaci su predati printeru."
	s := newNativeStudio(a)
	s.counts = map[string]int{"sent": 1}
	b, _ := json.Marshal(job)
	var row map[string]any
	_ = json.Unmarshal(b, &row)
	s.jobs = []map[string]any{row}
	for _, size := range [][2]int{{1500, 1000}, {1000, 650}} {
		capture := func(name string) {
			tt := ui.NewTester(s.view, size[0], size[1])
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%s-%d.png", name, int(size[0]))))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, page := range []string{"Printeri", "Predlošci", "Poslovi printanja", "API povezivanje", "Editor"} {
			s.page = page
			s.modal = ""
			if page == "Editor" {
				s.openEditor(a.ConfigSnapshot().Templates[0])
			}
			capture(page)
		}
		s.page = "Printeri"
		s.printer = p
		s.modal = "printer"
		capture("Printer-modal")
		s.printer.Kind = "usb"
		s.printer.Queue = "EPSON_TM_T20IV"
		s.queues = []string{s.printer.Queue}
		capture("USB-modal")
		s.openJob(job.ID)
		capture("Job-details")
		s.jobTab = "Pregled"
		capture("Job-preview")
		s.confirm("Ponovo isprintati račun?", func() {})
		capture("Confirmation")
		s.confirmAction = nil
		s.modal = ""
		s.openEditor(a.ConfigSnapshot().Templates[0])
		s.editor.doc.Selected = "items"
		capture("Table-properties")
		s.editor.tab = "Komande"
		capture("Editor-commands")
		s.editor.tab = "Podaci"
		capture("Editor-data")
	}
}

func TestNativeModalAndEditorNavigation(t *testing.T) {
	a := testAgent(t)
	addTestPrinter(t, a, "127.0.0.1", 9)
	s := newNativeStudio(a)
	tt := ui.NewTester(s.view, 1000, 650)
	if err := tt.Click("Postavke"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"Naziv printera", "Predložak računa", "Testiraj vezu", "Odustani", "Sačuvaj"} {
		r, ok := tt.Find(label)
		if !ok || r.Y < 0 || r.Y+r.H > 650 || r.X < 0 || r.X+r.W > 1000 {
			t.Fatalf("modal control %q outside window: %+v", label, r)
		}
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.modal != "" {
		t.Fatal("Escape did not close printer modal")
	}
	if err := tt.Click("Uredi predložak"); err != nil {
		t.Fatal(err)
	}
	if s.page != "Editor" {
		t.Fatal("editor did not open")
	}
	s.editor.doc.Set("", "name", "Promijenjeni naziv")
	tt.Frame()
	if err := tt.Click("Zatvori editor"); err != nil {
		t.Fatal(err)
	}
	if s.confirmAction == nil || s.page != "Editor" {
		t.Fatal("dirty editor discarded without confirmation")
	}
	if err := tt.Click("Odustani"); err != nil {
		t.Fatal(err)
	}
	if s.page != "Editor" || !s.dirty() {
		t.Fatal("cancel lost editor changes")
	}
	s.saved = string(s.editor.doc.JSON()) + string(s.editor.doc.Data)
	tt.Frame()
	if err := tt.Click("Zatvori editor"); err != nil {
		t.Fatal(err)
	}
	if s.page != "Predlošci" || s.editor != nil {
		t.Fatal("clean editor did not close")
	}
}

func TestNativeJobDetailsUseDirectSnapshots(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	job, err := a.PrintTestReceipt(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	s := newNativeStudio(a)
	s.openJob(job["id"].(string))
	tt := ui.NewTester(s.view, 1000, 850)
	for _, label := range []string{p.Name, "127.0.0.1:1", nativeTime(job["created_at"])} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("direct snapshot field missing from UI: %s", label)
		}
	}
	if err := tt.Click("Pregled"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.jobBitmap == nil {
		t.Fatal("direct job preview missing")
	}
	for _, closeLabel := range []string{"Zatvori detalje", "Zatvori"} {
		if err := tt.Click(closeLabel); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if s.modal != "" || s.jobBitmap != nil || s.job != nil {
			t.Fatal("closed job retained preview or snapshot")
		}
		s.openJob(job["id"].(string))
		tt.Frame()
		if err := tt.Click("Pregled"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if s.jobBitmap == nil {
			t.Fatal("reopening job lost preview")
		}
	}
	if mustJob(t, a, job["id"].(string)).Status != "queued" || countTestJobs(t, a) != 1 {
		t.Fatal("opening details changed print jobs")
	}
}

func TestNativeResizeAndProperties(t *testing.T) {
	enc, _ := receipt.NewEncoder()
	raw, data, _ := enc.Defaults()
	e, _ := newNativeEditor(enc, raw, data)
	tt := ui.NewTester(e.view, 1500, 1200)
	if err := tt.Click("Element number"); err != nil {
		t.Fatal(err)
	}
	handle, ok := tt.Find("Promijeni širinu number")
	if !ok {
		t.Fatal("resize handle missing")
	}
	tt.Press(handle.X+4, handle.Y+8)
	tt.Move(handle.X-70, handle.Y+8)
	tt.Release(handle.X-70, handle.Y+8)
	tt.Frame()
	if numberValue(e.doc.Element("number")["width"]) >= 100 {
		t.Fatal("resize did not change width")
	}
}

func TestNativeBackspaceInInputDoesNotDeleteElement(t *testing.T) {
	enc, _ := receipt.NewEncoder()
	raw, data, _ := enc.Defaults()
	e, _ := newNativeEditor(enc, raw, data)
	e.doc.Selected = "brand"
	tt := ui.NewTester(e.view, 1500, 1200)
	if err := tt.Click("Podaci"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Podaci računa"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyBackspace)
	if e.doc.Element("brand") == nil {
		t.Fatal("typing deleted selected receipt element")
	}
}

func TestNativeNumberEditBuffer(t *testing.T) {
	value := float64(8)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() {
			studioNumber(c, &value, 0, 120, 1).Label("Gap value")
			studioButton(c, "Done", false)
		})
	}, 400, 200)
	if err := tt.Click("Gap value"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if value != 7 {
		t.Fatalf("number ArrowDown: %v", value)
	}
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if value != 8 {
		t.Fatalf("number ArrowUp: %v", value)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("24")
	tt.Frame()
	if value != 24 {
		t.Fatalf("number edit = %v", value)
	}
	for _, invalid := range []string{"", "NaN", "999"} {
		tt.Key(ui.Cmd, ui.KeyA)
		tt.Key(0, ui.KeyBackspace)
		tt.Type(invalid)
		tt.Frame()
		if value != 24 {
			t.Fatalf("invalid input %q reached receipt: %v", invalid, value)
		}
	}
	if err := tt.Click("Done"); err != nil {
		t.Fatal(err)
	}
	value = 8 // undo/external model change must resynchronize the field
	tt.Frame()
	if err := tt.Click("Gap value"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("16")
	tt.Frame()
	if value != 16 {
		t.Fatalf("field did not synchronize after undo: %v", value)
	}
}

func TestNativeAPIExampleUsesDefaultPrintersTemplate(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 9)
	s := newNativeStudio(a)
	s.config.Templates = append([]model.ReceiptTemplate{{ID: "unused", SampleData: json.RawMessage(`{"wrong":true}`)}}, s.config.Templates...)
	_, request, response := s.apiExample(true)
	if !bytes.Contains([]byte(request), []byte(s.config.APIKey)) || !bytes.Contains([]byte(request), []byte(p.ID)) || bytes.Contains([]byte(request), []byte("wrong")) {
		t.Fatal("example does not use default printer and its sample")
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		t.Fatal(err)
	}
	if result["status"] != "queued" {
		t.Fatal("print example implies completed printing")
	}
	_, public, _ := s.apiExample(false)
	if bytes.Contains([]byte(public), []byte(s.config.APIKey)) {
		t.Fatal("example exposes key before copy")
	}
	s.language = "en"
	for _, tab := range []string{"Slanje računa", "Preuzimanje računa", "Status posla", "Status agenta"} {
		s.apiTab = tab
		_, example, response := s.apiExample(false)
		for _, word := range []string{"KLJUC", "racun-", "Čeka", "Podaci su", "skraćeni", "Domaća kafa"} {
			if bytes.Contains([]byte(example+response), []byte(word)) {
				t.Fatalf("untranslated example: %s", word)
			}
		}
		if !bytes.Contains([]byte(example), []byte("const API_KEY =")) {
			t.Fatal("missing stable API_KEY variable")
		}
		if tab == "Preuzimanje računa" {
			if !strings.Contains(example, "response.blob()") || !strings.Contains(example, "/receipt") || !strings.Contains(response, "Content-Type: image/png") || !strings.Contains(response, "Binary receipt image data") {
				t.Fatal("download example must use the binary response")
			}
		} else if err := json.Unmarshal([]byte(response), &result); err != nil {
			t.Fatal(err)
		}
	}
	custom := json.RawMessage(`{"shop":"MALA RADNJA","items":[{"name":"Domaća kafa"}],"custom":"Čuvaj me"}`)
	if !bytes.Equal(custom, s.apiSampleData(custom)) {
		t.Fatal("translation changed user sample data")
	}
}

func TestNativeDateTimePicker(t *testing.T) {
	value := "2026-11-16 10:30"
	tt := ui.NewTester(func(c *ui.Context) {
		*nativeLanguage(c) = "en"
		c.SetTheme(&studioTheme)
		ui.Column(c).Width(300).Padding(10).Children(func() { studioDateTimePicker(c, &value, "Vrijeme od", false) })
	}, 500, 650)
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	click("Start time")
	click("2026-11-05")
	if value != "2026-11-16 10:30" {
		t.Fatal("draft selection applied before confirmation")
	}
	click("Selected time · Hours")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("14")
	tt.Frame()
	click("Selected time · Minutes")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("45")
	tt.Frame()
	click("Select")
	if value != "2026-11-05 14:45" {
		t.Fatalf("date/time selection: %s", value)
	}
	click("Start time")
	click("Next month")
	click("2026-12-10")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if value != "2026-11-05 14:45" {
		t.Fatal("dismiss applied draft")
	}
	click("Start time")
	click("Clear")
	if value != "" {
		t.Fatal("clear did not remove bound")
	}
}

func TestNativeFilterAndEmptyScreens(t *testing.T) {
	s := newNativeStudio(testAgent(t))
	s.language = "en"
	tt := ui.NewTester(s.view, 1000, 650)
	if _, ok := tt.Find("Add printer"); ok {
		t.Fatal("duplicate add printer in empty state")
	}
	if _, ok := tt.Find("Add your first printer"); !ok {
		t.Fatal("missing empty state action")
	}
	s.config.Templates = nil
	s.page = "Predlošci"
	tt.Frame()
	if _, ok := tt.Find("Create your first template"); !ok {
		t.Fatal("missing template empty state action")
	}
	if dir := os.Getenv("RECEIPT_NATIVE_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		capture := func(name string) {
			t.Helper()
			f, err := os.Create(filepath.Join(dir, name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		capture("Templates-empty")
		s.page = "Poslovi printanja"
		s.from = "2026-11-16 10:30"
		tt.Frame()
		if err := tt.Click("Start time"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		capture("Jobs-date-picker")
	}
}

func TestNativeDataEditorAcceptsNewlines(t *testing.T) {
	enc, _ := receipt.NewEncoder()
	raw, data, _ := enc.Defaults()
	e, _ := newNativeEditor(enc, raw, data)
	e.tab = "Podaci"
	tt := ui.NewTester(e.view, 1000, 650)
	if err := tt.Click("Podaci računa"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("{")
	tt.Key(0, ui.KeyEnter)
	tt.Type("}")
	tt.Frame()
	if e.dataText != "{\n}" {
		t.Fatalf("sample editor must accept multiline text: %q", e.dataText)
	}
}

func TestNativeEditorTextActionsStayVisible(t *testing.T) {
	a := testAgent(t)
	s := newNativeStudio(a)
	s.openEditor(s.config.Templates[0])
	for _, tab := range []string{"Podaci", "Komande"} {
		s.editor.tab = tab
		tt := ui.NewTester(s.view, 1000, 650)
		action := "Primijeni podatke"
		if tab == "Komande" {
			action = "Izvezi komande"
		}
		r, ok := tt.Find(action)
		if !ok || r.Y+r.H > 650 {
			t.Fatalf("%s action clipped: %+v", tab, r)
		}
	}
}

func TestNativeEditorScrollReachesBothPanelEnds(t *testing.T) {
	a := testAgent(t)
	s := newNativeStudio(a)
	s.openEditor(s.config.Templates[0])
	s.editor.doc.Selected = "items"
	s.editor.zoom = 1.5
	tt := ui.NewTester(s.view, 1000, 650)
	tt.Scroll(900, 500, 0, 10000)
	tt.Frame()
	r, ok := tt.Find("Obriši element")
	if !ok || r.Y < 182 || r.Y+r.H > 650 {
		t.Errorf("inspector bottom is inaccessible: %+v", r)
	}
	tt.Scroll(500, 500, 0, 10000)
	tt.Frame()
	r, ok = tt.Find("Kraj računa")
	if !ok || r.Y < 182 || r.Y+r.H > 650 {
		t.Errorf("receipt bottom is inaccessible: %+v", r)
	}
}

func TestNativeVectorPreviewPreservesTableSpacing(t *testing.T) {
	enc, _ := receipt.NewEncoder()
	raw, data, _ := enc.Defaults()
	doc, _ := receipt.OpenNativeDocument(raw, data)
	plan, _, err := enc.NativeCompile(doc)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := nativeReceiptSVG(plan, doc)
	if err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Background(studioWhite).Children(func() { ui.Image(c, preview.Tiles[0].SVG).Size(640, preview.Tiles[0].Height) })
	}, 640, 512)
	y := 0
	for _, b := range plan.Blocks {
		if b.ID == "items" {
			y = b.Lines[1].Y
		}
	}
	img := tt.Image()
	ink := 0
	// The first item's price occupies the final four printer cells. Collapsing
	// padding spaces in SVG moves it into the name column and leaves this blank.
	for py := y; py < y+24; py++ {
		for x := 560; x < 608; x++ {
			r, g, b, _ := img.At(x, py).RGBA()
			if r < 40000 && g < 40000 && b < 40000 {
				ink++
			}
		}
	}
	if ink < 10 {
		t.Fatalf("price disappeared from its column: %d ink pixels", ink)
	}
}
