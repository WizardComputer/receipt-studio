package model

import (
	"encoding/json"
	"time"
)

type FailureKind int

const (
	InvalidInput FailureKind = iota
	NotFound
	Conflict
	InvalidReceipt
	StorageFailure
	ConnectionFailure
)

type ActionError struct {
	Kind    FailureKind
	message string
}

func ActionFailure(kind FailureKind, message string) error { return &ActionError{kind, message} }

// Native callers own their snapshots, just as remote clients own decoded JSON.
// Mutating an editor's document must never change committed agent state.
func CloneTemplate(t ReceiptTemplate) ReceiptTemplate {
	t.Template = append(json.RawMessage(nil), t.Template...)
	t.SampleData = append(json.RawMessage(nil), t.SampleData...)
	return t
}

func CloneConfig(c Config) Config {
	c.Printers = append([]Printer(nil), c.Printers...)
	c.Templates = append([]ReceiptTemplate(nil), c.Templates...)
	for i := range c.Templates {
		c.Templates[i] = CloneTemplate(c.Templates[i])
	}
	return c
}

func ClonePNG(p *ReceiptPNG) *ReceiptPNG {
	if p == nil {
		return nil
	}
	copy := *p
	copy.Data = append([]byte(nil), p.Data...)
	return &copy
}

func CloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	copy := *t
	return &copy
}
func (e *ActionError) Error() string { return e.message }
