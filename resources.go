// Package resources embeds application metadata and trusted runtime assets.
// Go embed paths cannot traverse parent directories; application logic lives in internal.
package resources

import (
	"embed"
	"encoding/json"
)

//go:embed src/core.js src/cp852.js src/rpe-environment.js src/escpos-preview.js src/graphics.js src/vendor/* locales/en.json packaging/assets/tray.png
var Assets embed.FS

// mygo.json is the version source for both packaged builds and development runs.
//
//go:embed mygo.json
var ApplicationMetadata []byte

var AppVersion = func() string {
	var metadata struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(ApplicationMetadata, &metadata); err != nil {
		panic(err)
	}
	if metadata.Version == "" {
		panic("mygo.json: application version is required")
	}
	return metadata.Version
}()
