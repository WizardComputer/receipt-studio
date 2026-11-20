package model

// JSON encodes Data as Base64. Persist the artifact with the job so retries
// return the same PNG even after changing a template or updating the renderer.
type ReceiptPNG struct {
	ContentType  string  `json:"content_type"`
	Encoding     string  `json:"encoding"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	DPI          float64 `json:"dpi"`
	PaperWidthMM int     `json:"paper_width_mm"`
	MarginLeft   int     `json:"margin_left"`
	MarginRight  int     `json:"margin_right"`
	Scale        int     `json:"scale"`
	Data         []byte  `json:"data"`
}
