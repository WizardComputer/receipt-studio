package agent

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"

	resources "receiptstudio.local/agent"
)

func TestApplicationVersion(t *testing.T) {
	if !regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`).MatchString(resources.AppVersion) {
		t.Fatalf("invalid application version %q", resources.AppVersion)
	}
	for _, name := range []string{"mygo.json", "package.json", "package-lock.json"} {
		raw, err := os.ReadFile("../../" + name)
		if err != nil {
			t.Fatal(err)
		}
		var metadata struct {
			Version  string `json:"version"`
			Packages map[string]struct {
				Version string `json:"version"`
			} `json:"packages"`
		}
		if err := json.Unmarshal(raw, &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata.Version != resources.AppVersion {
			t.Fatalf("%s version %q differs from embedded %q", name, metadata.Version, resources.AppVersion)
		}
		if name == "package-lock.json" && metadata.Packages[""].Version != resources.AppVersion {
			t.Fatal("lockfile root package version differs")
		}
	}
	a := testAgent(t)
	response := request(a, "GET", "/v1/health", nil, a.config.APIKey, "")
	var health struct {
		Version    int    `json:"version"`
		AppVersion string `json:"app_version"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &health) != nil || health.Version != 1 || health.AppVersion != resources.AppVersion {
		t.Fatalf("wrong health version: %s", response.Body.String())
	}
}
