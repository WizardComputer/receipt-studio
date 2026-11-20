package receipt

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestNativeEditorMatchesFrozenBrowserReceipts(t *testing.T) {
	enc, err := NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../testdata/encoder.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name           string
		Template, Data json.RawMessage
		Hex            string
		Plan           NativePlan
	}
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			d, err := OpenNativeDocument(f.Template, f.Data)
			if err != nil {
				t.Fatal(err)
			}
			plan, b, err := enc.NativeCompile(d)
			if err != nil {
				t.Fatal(err)
			}
			if hex.EncodeToString(b) != f.Hex {
				t.Fatal("native editor changed frozen browser command bytes")
			}
			a, _ := json.Marshal(plan)
			want, _ := json.Marshal(f.Plan)
			if !bytes.Equal(a, want) {
				t.Fatalf("hit geometry differs from browser: %s vs %s", a, want)
			}
			// Editing then undoing must preserve the exact bytes, not merely the look.
			d.Set("", "name", "Changed")
			d.Undo()
			_, after, err := enc.NativeCompile(d)
			if err != nil || !bytes.Equal(b, after) {
				t.Fatal("undo changed bytes", err)
			}
		})
	}
}

func TestNativeEditorMoveResizeDeleteUndo(t *testing.T) {
	enc, _ := NewEncoder()
	raw, data, _ := enc.Defaults()
	d, _ := OpenNativeDocument(raw, data)
	original := string(d.JSON())
	d.Move("brand", "items")
	if d.Elements()[3].(map[string]any)["id"] != "brand" {
		t.Fatal("drop insertion order")
	}
	d.Geometry("brand", 95, 100)
	e := d.Element("brand")
	if e["x"] != float64(90) || e["width"] != float64(10) {
		t.Fatal("geometry escaped printable area")
	}
	d.Delete()
	if d.Element("brand") != nil {
		t.Fatal("delete failed")
	}
	d.Undo()
	d.Undo()
	d.Undo()
	if string(d.JSON()) != original {
		t.Fatal("undo did not restore original")
	}
	d.Redo()
	if d.Elements()[3].(map[string]any)["id"] != "brand" {
		t.Fatal("redo failed")
	}
}

func TestNativeUndoRemovesOptionalFields(t *testing.T) {
	enc, _ := NewEncoder()
	raw, data, _ := enc.Defaults()
	d, _ := OpenNativeDocument(raw, data)
	before := string(d.JSON())
	d.Set("", "language", "star-prnt")
	d.Undo()
	if string(d.JSON()) != before {
		t.Fatal("undo retained field absent from original template")
	}
}
