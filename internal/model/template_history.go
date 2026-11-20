package model

type TemplateRevision struct {
	ReceiptTemplate
	CreatedAt    string `json:"created_at"`
	RestoredFrom int    `json:"restored_from,omitempty"`
}

type RevisionSummary struct {
	Version      int    `json:"version"`
	CreatedAt    string `json:"created_at"`
	RestoredFrom int    `json:"restored_from,omitempty"`
}

type RevisionPage struct {
	Versions   []RevisionSummary `json:"versions"`
	NextBefore int               `json:"next_before,omitempty"`
}
