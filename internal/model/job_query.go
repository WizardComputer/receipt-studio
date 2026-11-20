package model

import (
	"time"
)

type JobQuery struct {
	Status   string
	From, To time.Time
	Before   string
	Limit    int
}

type JobPage struct {
	Items  []map[string]any `json:"items"`
	Cursor string           `json:"next_cursor"`
	Total  int              `json:"total"`
	Counts map[string]int   `json:"counts"`
}
