package model

import (
	"encoding/json"
	"errors"
	"net"
	"time"
)

type Printer struct {
	Connection
	ID         string `json:"id"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	TemplateID string `json:"template_id"`
	Revision   int    `json:"revision"`
}

type ReceiptTemplate struct {
	ID         string          `json:"id"`
	Version    int             `json:"version"`
	Template   json.RawMessage `json:"template"`
	SampleData json.RawMessage `json:"sample_data"`
}

type Config struct {
	APIKey         string            `json:"api_key"`
	DefaultPrinter string            `json:"default_printer"`
	Printers       []Printer         `json:"printers"`
	Templates      []ReceiptTemplate `json:"templates"`
}

type PrintRequest struct {
	Version   int             `json:"version"`
	JobID     string          `json:"job_id"`
	PrinterID string          `json:"printer_id"`
	Data      json.RawMessage `json:"data"`
	ReturnPNG bool            `json:"return_png,omitempty"`
}

type Job struct {
	Connection
	ID              string          `json:"id"`
	PrinterID       string          `json:"printer_id"`
	PrinterName     string          `json:"printer_name"`
	TemplateVersion int             `json:"template_version"`
	Status          string          `json:"status"`
	Message         string          `json:"message"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	StartedAt       *time.Time      `json:"started_at,omitempty"`
	CompletedAt     *time.Time      `json:"completed_at,omitempty"`
	OriginalJobID   string          `json:"original_job_id,omitempty"`
	ByteCount       int             `json:"byte_count"`
	Host            string          `json:"host"`
	Port            int             `json:"port"`
	Template        json.RawMessage `json:"template"`
	Data            json.RawMessage `json:"data"`
	Bytes           []byte          `json:"bytes"`
	PNG             *ReceiptPNG     `json:"png,omitempty"`
}

func ValidID(id string) bool {
	if len(id) < 1 || len(id) > 100 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func ValidateAddress(host string, port int) error {
	ip := net.ParseIP(host)
	if ip == nil || !(ip.IsPrivate() || ip.IsLoopback()) || port < 1 || port > 65535 {
		return errors.New("Unesi privatnu LAN IP adresu printera i port 1–65535.")
	}
	return nil
}

func CompactJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func JobSummary(j *Job) map[string]any {
	return map[string]any{"id": j.ID, "printer_id": j.PrinterID, "printer_name": j.PrinterName, "template_version": j.TemplateVersion, "status": j.Status, "message": j.Message, "created_at": j.CreatedAt, "updated_at": j.UpdatedAt, "byte_count": j.ByteCount}
}

var ErrConflict = errors.New("job_id već postoji s drugim podacima ili printerom. Za novo printanje koristi novi ID.")
