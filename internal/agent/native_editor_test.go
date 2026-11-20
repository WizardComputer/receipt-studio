package agent

import (
	"bytes"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/receipt"
	"receiptstudio.local/agent/internal/storage"
)

// Optional local validation uses existing templates and samples without copying
// customer data into the repository or sending a print job.
func TestNativeExistingAgentTemplates(t *testing.T) {
	path := os.Getenv("RECEIPT_NATIVE_CONFIG")
	if path == "" {
		t.Skip("no existing agent config requested")
	}
	config := readNativeTestConfig(t, path)
	enc, _ := receipt.NewEncoder()
	for _, entry := range config.Templates {
		t.Run(entry.ID, func(t *testing.T) {
			old, err := enc.Compile(entry.Template, entry.SampleData)
			if err != nil {
				t.Fatal(err)
			}
			d, err := receipt.OpenNativeDocument(entry.Template, entry.SampleData)
			if err != nil {
				t.Fatal(err)
			}
			_, native, err := enc.NativeCompile(d)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(old, native) {
				t.Fatal("existing receipt changed")
			}
			oldPNG, err := enc.RenderPNG(entry.Template, old)
			if err != nil {
				t.Fatal(err)
			}
			nativePNG, err := enc.RenderPNG(d.JSON(), native)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(oldPNG.Data, nativePNG.Data) {
				t.Fatal("existing receipt preview changed")
			}
			t.Logf("%d identical command bytes and identical PNG", len(native))
		})
	}
}

// Read-only validation uses the agent's SQLite database.
func readNativeTestConfig(t *testing.T, path string) model.Config {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	uri := url.URL{Scheme: "file", Path: absolute, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	config, err := storage.ReadConfig(db)
	if err != nil {
		t.Fatal(err)
	}
	return config
}
