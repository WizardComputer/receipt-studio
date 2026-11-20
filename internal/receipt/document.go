package receipt

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/dop251/goja"
)

// NativeDocument retains the JSON schema and uses the same RPE pipeline as the
// web designer. Screen geometry never becomes a second receipt layout engine.
type NativeDocument struct {
	Template   map[string]any
	Data       json.RawMessage
	Selected   string
	undo, redo []nativeSnapshot
	gesture    *nativeSnapshot
}

type nativeSnapshot struct {
	Template json.RawMessage
	Data     json.RawMessage
	Selected string
}

func OpenNativeDocument(template, data json.RawMessage) (*NativeDocument, error) {
	d := &NativeDocument{Data: append(json.RawMessage(nil), data...)}
	if err := json.Unmarshal(template, &d.Template); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *NativeDocument) JSON() json.RawMessage { b, _ := json.Marshal(d.Template); return b }

func (d *NativeDocument) snapshot() nativeSnapshot {
	return nativeSnapshot{d.JSON(), append(json.RawMessage(nil), d.Data...), d.Selected}
}

func (d *NativeDocument) restore(s nativeSnapshot) {
	d.Template = nil
	_ = json.Unmarshal(s.Template, &d.Template)
	d.Data = append(json.RawMessage(nil), s.Data...)
	d.Selected = s.Selected
}

func (d *NativeDocument) Change(fn func()) {
	if d.gesture != nil {
		fn()
		return
	}
	before := d.snapshot()
	fn()
	d.record(before)
}

func (d *NativeDocument) record(before nativeSnapshot) {
	if string(before.Template) == string(d.JSON()) && string(before.Data) == string(d.Data) {
		return
	}
	d.undo = append(d.undo, before)
	if len(d.undo) > 100 {
		d.undo = d.undo[1:]
	}
	d.redo = nil
}

func (d *NativeDocument) BeginGesture() {
	if d.gesture == nil {
		before := d.snapshot()
		d.gesture = &before
	}
}

func (d *NativeDocument) EndGesture(cancel bool) {
	if d.gesture == nil {
		return
	}
	before := *d.gesture
	d.gesture = nil
	if cancel {
		d.restore(before)
	} else {
		d.record(before)
	}
}

func (d *NativeDocument) Undo() {
	if len(d.undo) == 0 {
		return
	}
	d.redo = append(d.redo, d.snapshot())
	s := d.undo[len(d.undo)-1]
	d.undo = d.undo[:len(d.undo)-1]
	d.restore(s)
}

func (d *NativeDocument) Redo() {
	if len(d.redo) == 0 {
		return
	}
	d.undo = append(d.undo, d.snapshot())
	s := d.redo[len(d.redo)-1]
	d.redo = d.redo[:len(d.redo)-1]
	d.restore(s)
}

func (d *NativeDocument) Elements() []any { v, _ := d.Template["elements"].([]any); return v }

// Names are editor metadata; older templates can still use their text as a label.
func (d *NativeDocument) ElementName(el map[string]any) string {
	if name, _ := el["name"].(string); strings.TrimSpace(name) != "" {
		return name
	}
	if el["type"] == "text" {
		if text, _ := el["text"].(string); strings.TrimSpace(text) != "" {
			return strings.Join(strings.Fields(text), " ")
		}
	}
	n := 0
	for _, v := range d.Elements() {
		other := v.(map[string]any)
		if other["type"] == el["type"] {
			n++
		}
		if other["id"] == el["id"] {
			break
		}
	}
	return fmt.Sprintf("%s %d", NativeElementKindName(el), max(1, n))
}

func NativeElementKindName(el map[string]any) string {
	kind, _ := el["type"].(string)
	if name := map[string]string{"text": "Tekst", "table": "Tabela", "qr": "QR kod", "image": "Slika"}[kind]; name != "" {
		return name
	}
	return "Element"
}

func (d *NativeDocument) Element(id string) map[string]any {
	for _, v := range d.Elements() {
		e := v.(map[string]any)
		if e["id"] == id {
			return e
		}
	}
	return nil
}

func (d *NativeDocument) Set(id, key string, value any) {
	d.Change(func() {
		if id == "" {
			d.Template[key] = value
		} else if e := d.Element(id); e != nil {
			e[key] = value
		}
	})
}

func (d *NativeDocument) SetData(raw string) error {
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return err
	}
	if value == nil {
		return fmt.Errorf("Podaci moraju biti JSON objekt")
	}
	d.Change(func() { d.Data = json.RawMessage(raw) })
	return nil
}

func (d *NativeDocument) Delete() {
	d.Change(func() {
		items := d.Elements()
		for i, v := range items {
			if v.(map[string]any)["id"] == d.Selected {
				d.Template["elements"] = append(items[:i:i], items[i+1:]...)
				d.Selected = ""
				return
			}
		}
	})
}

func (d *NativeDocument) Insert(e map[string]any, before string) {
	d.Change(func() {
		items := d.Elements()
		if name, _ := e["name"].(string); strings.TrimSpace(name) == "" {
			used := make(map[string]bool)
			for _, v := range items {
				used[d.ElementName(v.(map[string]any))] = true
			}
			for n := 1; ; n++ {
				name := fmt.Sprintf("%s %d", NativeElementKindName(e), n)
				if !used[name] {
					e["name"] = name
					break
				}
			}
		}
		at := len(items)
		for i, v := range items {
			if v.(map[string]any)["id"] == before {
				at = i
				break
			}
		}
		items = append(items, nil)
		copy(items[at+1:], items[at:])
		items[at] = e
		d.Template["elements"] = items
		d.Selected = e["id"].(string)
	})
}

func (d *NativeDocument) Move(id, before string) {
	if id == before {
		return
	}
	d.Change(func() {
		items := d.Elements()
		out := make([]any, 0, len(items))
		var moved any
		for _, v := range items {
			if v.(map[string]any)["id"] == id {
				moved = v
			} else {
				out = append(out, v)
			}
		}
		if moved == nil {
			return
		}
		at := len(out)
		for i, v := range out {
			if v.(map[string]any)["id"] == before {
				at = i
				break
			}
		}
		out = append(out, nil)
		copy(out[at+1:], out[at:])
		out[at] = moved
		d.Template["elements"] = out
		d.Selected = id
	})
}

func (d *NativeDocument) Geometry(id string, x, width float64) {
	e := d.Element(id)
	if e == nil || e["type"] == "qr" && e["qrMode"] == "native" {
		return
	}
	x = math.Max(0, math.Min(90, x))
	width = math.Max(10, math.Min(100-x, width))
	d.Change(func() { e["x"] = x; e["width"] = width })
}

func (e *Encoder) NativeElement(kind, id string) (map[string]any, error) {
	v, err := e.run("JSON.stringify(newElement("+strconvQuote(kind)+","+strconvQuote(id)+"))", nil)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	err = json.Unmarshal([]byte(v.String()), &result)
	return result, err
}

type NativeBlock struct {
	ID                  string
	X, Y, Width, Height float64
	Lines               []receiptLine
	Graphics            []receiptGraphic
}

type NativePlan struct {
	Width, Height float64
	Blocks        []NativeBlock
}

func (e *Encoder) NativeCompile(d *NativeDocument) (NativePlan, []byte, error) {
	var plan NativePlan
	v, err := e.run("(()=>{const r=compile(jobTemplate,jobData);return {plan:JSON.stringify(r.plan),bytes:r.bytes}})()", map[string]json.RawMessage{"jobTemplate": d.JSON(), "jobData": d.Data})
	if err != nil {
		return plan, nil, err
	}
	result, ok := v.(*goja.Object)
	if !ok {
		return plan, nil, fmt.Errorf("Encoder nije vratio plan")
	}
	if err = json.Unmarshal([]byte(result.Get("plan").String()), &plan); err != nil {
		return plan, nil, err
	}
	b, err := copyEncoderBytes(result.Get("bytes"))
	return plan, b, err
}

// CanUndo and CanRedo keep history internals private to the document.
func (d *NativeDocument) CanUndo() bool { return len(d.undo) > 0 }
func (d *NativeDocument) CanRedo() bool { return len(d.redo) > 0 }
