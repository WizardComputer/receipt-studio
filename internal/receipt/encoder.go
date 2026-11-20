package receipt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"

	resources "receiptstudio.local/agent"
)

// Compile the trusted, embedded source once. Every job gets an isolated runtime.
// Templates and receipt data are JSON, never executable JavaScript.

type Encoder struct{ program *goja.Program }

func NewEncoder() (*Encoder, error) {
	core, err := resources.Assets.ReadFile("src/core.js")
	if err != nil {
		return nil, err
	}
	page, err := resources.Assets.ReadFile("src/cp852.js")
	if err != nil {
		return nil, err
	}
	vendor, err := resources.Assets.ReadFile("src/vendor/receipt-printer-encoder-4.0.1.js")
	if err != nil {
		return nil, err
	}
	environment, err := resources.Assets.ReadFile("src/rpe-environment.js")
	if err != nil {
		return nil, err
	}
	preview, err := resources.Assets.ReadFile("src/escpos-preview.js")
	if err != nil {
		return nil, err
	}
	qr, err := resources.Assets.ReadFile("src/vendor/qrcode-generator-2.0.4.js")
	if err != nil {
		return nil, err
	}
	graphics, err := resources.Assets.ReadFile("src/graphics.js")
	if err != nil {
		return nil, err
	}
	stripModule := func(source string) string {
		lines := strings.Split(source, "\n")
		kept := []string{}
		for _, line := range lines {
			if strings.HasPrefix(line, "import ") {
				continue
			}
			kept = append(kept, strings.TrimPrefix(line, "export "))
		}
		return strings.Join(kept, "\n")
	}
	source := "Object.hasOwn = Object.hasOwn || ((obj, key) => Object.prototype.hasOwnProperty.call(obj, key));\n" +
		string(environment) + "\n" + string(vendor) + "\n" + string(qr) + "\n" + stripModule(string(graphics)) + "\n" + stripModule(string(page)) + "\n" +
		stripModule(string(preview)) + "\n" + stripModule(string(core))
	program, err := goja.Compile("receipt-core.js", source, true)
	return &Encoder{program: program}, err
}

func (e *Encoder) run(expression string, values map[string]json.RawMessage) (goja.Value, error) {
	return e.runWithBytes(expression, values, nil)
}

func (e *Encoder) runWithBytes(expression string, values map[string]json.RawMessage, buffers map[string][]byte) (goja.Value, error) {
	vm := goja.New()
	// QR error correction and pixel packing need more time than text in Goja.
	// Keep a hard deadline; template/image dimensions are bounded in core.js.
	timer := time.AfterFunc(10*time.Second, func() { vm.Interrupt("Izračun računa je trajao predugo.") })
	defer timer.Stop()
	if _, err := vm.RunProgram(e.program); err != nil {
		return nil, err
	}
	parse, ok := goja.AssertFunction(vm.Get("JSON").ToObject(vm).Get("parse"))
	if !ok {
		return nil, fmt.Errorf("JSON.parse nije dostupan")
	}
	for key, raw := range values {
		if !json.Valid(raw) {
			return nil, fmt.Errorf("Neispravan JSON: %s", key)
		}
		// Pass JSON as data, never executable source. Parsing once in the VM
		// preserves native JS object/array semantics without intermediate Go maps.
		v, err := parse(goja.Undefined(), vm.ToValue(string(raw)))
		if err != nil {
			return nil, err
		}
		if err := vm.Set(key, v); err != nil {
			return nil, err
		}
	}
	for key, data := range buffers {
		// The VM owns its copy, so JS cannot mutate the caller's stored commands.
		if err := vm.Set(key, vm.NewArrayBuffer(bytes.Clone(data))); err != nil {
			return nil, err
		}
	}
	return vm.RunString(expression)
}

func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }

func (e *Encoder) Compile(template, data json.RawMessage) ([]byte, error) {
	value, err := e.run("compile(jobTemplate, jobData, {preview:false}).bytes", map[string]json.RawMessage{"jobTemplate": template, "jobData": data})
	if err != nil {
		return nil, err
	}
	return copyEncoderBytes(value)
}

func copyEncoderBytes(value goja.Value) ([]byte, error) {
	data, ok := value.Export().([]byte)
	if !ok {
		return nil, fmt.Errorf("Encoder nije vratio Uint8Array")
	}
	// Export aliases the typed array's backing store. Detach the result from
	// runtime-owned storage, including the correct offset/length of a subarray.
	return bytes.Clone(data), nil
}

func (e *Encoder) Validate(template json.RawMessage) error {
	_, err := e.run("validateTemplate(jobTemplate)", map[string]json.RawMessage{"jobTemplate": template})
	return err
}

func (e *Encoder) Defaults() (json.RawMessage, json.RawMessage, error) {
	v, err := e.run("JSON.stringify({template: initialTemplate(), data: sampleData})", nil)
	if err != nil {
		return nil, nil, err
	}
	var defaults struct{ Template, Data json.RawMessage }
	err = json.Unmarshal([]byte(v.String()), &defaults)
	return defaults.Template, defaults.Data, err
}
