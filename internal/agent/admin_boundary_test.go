package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdministrationIsNotExposedOverHTTP(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	before, _ := json.Marshal(a.config)
	routes := []string{
		"GET /api/bootstrap", "GET /api/config", "GET /api/preferences", "PUT /api/preferences",
		"POST /api/token", "GET /api/defaults", "GET /api/devices",
		"POST /api/printers", "PUT /api/printers/" + p.ID,
		"POST /api/printer-connection", "POST /api/printers/" + p.ID + "/connection", "POST /api/printers/" + p.ID + "/test",
		"POST /api/templates", "PUT /api/templates/" + p.TemplateID, "DELETE /api/templates/" + p.TemplateID,
		"POST /api/templates/import", "GET /api/templates/" + p.TemplateID + "/export",
		"GET /api/templates/" + p.TemplateID + "/versions", "GET /api/templates/" + p.TemplateID + "/versions/1",
		"GET /api/templates/" + p.TemplateID + "/versions/1/preview", "POST /api/templates/" + p.TemplateID + "/versions/1/restore",
		"GET /api/diagnostics", "GET /api/diagnostics/1", "GET /api/jobs", "GET /api/jobs/example", "GET /api/jobs/example/preview", "POST /api/jobs/example/reprint",
		"OPTIONS /api/config", "GET /", "GET /designer", "GET /agent-ui/app.js",
	}
	server := httptest.NewUnstartedServer(nil)
	a.host = server.Listener.Addr().String()
	a.origin = "http://" + a.host
	server.Config.Handler = a.handler()
	server.Start()
	defer server.Close()
	for _, route := range routes {
		method, path, _ := strings.Cut(route, " ")
		for _, token := range []string{"", a.config.APIKey, "wrong-admin-key"} {
			for _, origin := range []string{"", a.origin, "https://app.example"} {
				r, err := http.NewRequest(method, server.URL+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				r.Header.Set("Authorization", "Bearer "+token)
				r.Header.Set("Origin", origin)
				r.Header.Set("X-Agent-UI", "1")
				response, err := server.Client().Do(r)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != http.StatusNotFound {
					t.Fatalf("%s exposed over HTTP: status %d", route, response.StatusCode)
				}
				if strings.Contains(string(body), "wrong-admin-key") || strings.Contains(string(body), a.config.APIKey) {
					t.Fatalf("%s leaked a credential", route)
				}
				if response.Header.Get("Access-Control-Allow-Origin") != "" {
					t.Fatalf("%s advertises browser access", route)
				}
			}
		}
	}
	after, _ := json.Marshal(a.config)
	if string(before) != string(after) || countTestJobs(t, a) != 0 {
		t.Fatal("network administration modified agent state")
	}
	if a.ConfigSnapshot().APIKey != a.config.APIKey {
		t.Fatal("native configuration unavailable")
	}
}
