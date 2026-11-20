//go:build desktop

package desktop

import (
	"bytes"
	"encoding/xml"
	"io"
	"reflect"
	"testing"
)

func TestLoginArgumentsArePreservedAndEscaped(t *testing.T) {
	executable := "/Applications/A & B/Receipt Studio.app/Contents/MacOS/receipt-agent"
	args := []string{"--data-dir", "/Users/Test/Folder <one>", "--listen", "127.0.0.1:17840"}
	data := launchAgentPlist(executable, args)
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var values []string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "string" {
			var text string
			if err = decoder.DecodeElement(&text, &start); err != nil {
				t.Fatal(err)
			}
			values = append(values, text)
		}
	}
	expected := append([]string{"com.wizardhealth.receiptstudio", executable}, args...)
	expected = append(expected, "Aqua")
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("arguments changed: %v", values)
	}
}
