//go:build desktop

package desktop

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "com.wizardhealth.receiptstudio.plist"), err
}

func plistString(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return "<string>" + b.String() + "</string>"
}

func launchAgentPlist(executable string, args []string) []byte {
	arguments := plistString(executable)
	for _, arg := range args {
		arguments += plistString(arg)
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>Label</key><string>com.wizardhealth.receiptstudio</string><key>ProgramArguments</key><array>` + arguments + `</array><key>RunAtLoad</key><true/><key>LimitLoadToSessionType</key><string>Aqua</string></dict></plist>`)
}

func autostartEnabled() bool {
	path, err := launchAgentPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func setAutostart(enable bool, args []string) error {
	path, err := launchAgentPath()
	if err != nil {
		return err
	}
	if !enable {
		err = os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".receipt-startup-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(launchAgentPlist(executable, args)); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func openSettings(url string) error { return exec.Command("/usr/bin/open", url).Run() }

func desktopAlert(message string) {
	script := `on run argv
 display alert "Receipt Studio" message (item 1 of argv) as warning
end run`
	if err := exec.Command("/usr/bin/osascript", "-e", script, message).Run(); err != nil {
		fmt.Fprintln(os.Stderr, message)
	}
}
