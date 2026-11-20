package agent

import (
	"encoding/json"
	"testing"

	"receiptstudio.local/agent/internal/model"
)

func TestLanguagePreferenceIsPrivateAndDurable(t *testing.T) {
	a := testAgent(t)
	before, _ := json.Marshal(a.config)
	for _, path := range []string{"/api/preferences"} {
		if r := request(a, "GET", path, nil, a.config.APIKey, ""); r.Code != 404 {
			t.Fatal("public key can read preferences")
		}
		if r := request(a, "PUT", path, model.Preferences{Language: "en"}, a.config.APIKey, ""); r.Code != 404 {
			t.Fatal("public key can write preferences")
		}
	}
	requireActionFailure(t, a.SavePreferences(model.Preferences{Language: "de"}), model.InvalidInput)
	if err := a.SavePreferences(model.Preferences{Language: "en"}); err != nil {
		t.Fatal(err)
	}
	b, err := openTestAgent(t, a.dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.store.Preferences()
	if err != nil || p.Language != "en" {
		t.Fatal("language lost on restart", p, err)
	}
	after, _ := json.Marshal(b.config)
	if string(before) != string(after) {
		t.Fatal("language preference changed receipt/config content")
	}
	var count int
	b.db.QueryRow(`SELECT count(*) FROM template_versions`).Scan(&count)
	if count != 1 {
		t.Fatal("language change created a template revision")
	}
}
