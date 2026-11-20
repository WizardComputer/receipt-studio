package receipt

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestEncoderMatchesBrowserBytes(t *testing.T) {
	encoder, err := NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("../../testdata/encoder.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name           string
		Template, Data json.RawMessage
		Plan           json.RawMessage
		Hex            string
	}
	if err = json.Unmarshal(fixture, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			actual, err := encoder.Compile(c.Template, c.Data)
			if err != nil {
				t.Fatal(err)
			}
			if hex.EncodeToString(actual) != c.Hex {
				t.Fatal("Go agent bytes differ from browser encoder")
			}
			plan, err := encoder.run("JSON.stringify(compile(jobTemplate, jobData).plan)", map[string]json.RawMessage{"jobTemplate": c.Template, "jobData": c.Data})
			if err != nil {
				t.Fatal(err)
			}
			var expected, received any
			_ = json.Unmarshal(c.Plan, &expected)
			_ = json.Unmarshal([]byte(plan.String()), &received)
			e, _ := json.Marshal(expected)
			r, _ := json.Marshal(received)
			if !bytes.Equal(e, r) {
				t.Fatal("Goja and browser preview layouts differ")
			}

		})
	}
}
