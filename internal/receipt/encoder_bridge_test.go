package receipt

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/dop251/goja"
)

func TestEncoderJSONIsDataAndKeepsNativeSemantics(t *testing.T) {
	e, err := NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	input := json.RawMessage(`{"text":"\"; throw new Error('injected'); // \\ čćžšđ\n","items":[1,true,null],"id":"9007199254740993","__proto__":{"polluted":true}}`)
	v, err := e.run(`JSON.stringify({text:payload.text, array:Array.isArray(payload.items), id:payload.id, own:Object.hasOwn(payload,'__proto__'), polluted:({}).polluted === true})`, map[string]json.RawMessage{"payload": input})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Text, ID             string
		Array, Own, Polluted bool
	}
	if err := json.Unmarshal([]byte(v.String()), &got); err != nil {
		t.Fatal(err)
	}
	var original struct{ Text string }
	if err := json.Unmarshal(input, &original); err != nil {
		t.Fatal(err)
	}
	if got.Text != original.Text || got.ID != "9007199254740993" || !got.Array || !got.Own || got.Polluted {
		t.Fatalf("JSON bridge changed data/semantics: %+v", got)
	}
	for _, invalid := range []string{"", "{", `{}; throw new Error('injected')`, `{"x":NaN}`} {
		if _, err := e.run("payload", map[string]json.RawMessage{"payload": json.RawMessage(invalid)}); err == nil {
			t.Fatalf("accepted invalid JSON %q", invalid)
		}
	}
}

func TestEncoderBinaryBridgeOwnsCopiesAndHonorsViewBounds(t *testing.T) {
	e, err := NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	input := []byte{0, 27, 128, 255, 10}
	v, err := e.runWithBytes(`(()=>{const a=new Uint8Array(buffer);a[1]=42;return a.subarray(1,4)})()`, nil, map[string][]byte{"buffer": input})
	if err != nil {
		t.Fatal(err)
	}
	got, err := copyEncoderBytes(v)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{42, 128, 255}) || !bytes.Equal(input, []byte{0, 27, 128, 255, 10}) {
		t.Fatalf("binary bridge changed input or view: input=%v output=%v", input, got)
	}
	got[0] = 99
	if v.Export().([]byte)[0] != 42 {
		t.Fatal("output aliases VM storage")
	}
	if _, err := copyEncoderBytes(goja.Undefined()); err == nil {
		t.Fatal("accepted non-byte result")
	}
}
