package agent

import (
	"fmt"
	"testing"
	"time"

	"receiptstudio.local/agent/internal/model"
)

func TestJobFiltersBeforeLimitAndTimeBoundaries(t *testing.T) {
	a := testAgent(t)
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 130; i++ {
		id := fmt.Sprintf("j-%03d", i)
		status := "sent"
		if i < 3 {
			status = "failed"
		}
		putTestJob(t, a, &model.Job{ID: id, Status: status, CreatedAt: base.Add(time.Duration(i) * time.Hour)})
	}
	offset := time.FixedZone("UTC+1", 3600)
	cases := []struct {
		name  string
		query model.JobQuery
		count int
		first string
	}{
		{"default limit", model.JobQuery{}, 100, "j-129"},
		{"status", model.JobQuery{Status: "failed"}, 3, "j-002"},
		{"time boundaries", model.JobQuery{Status: "failed", From: base.Add(time.Hour), To: base.Add(2 * time.Hour)}, 1, "j-001"},
		{"timezone", model.JobQuery{From: time.Date(2026, 11, 1, 2, 0, 0, 0, offset), To: time.Date(2026, 11, 1, 3, 0, 0, 0, offset)}, 1, "j-001"},
		{"no matches", model.JobQuery{Status: "unknown"}, 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			page, err := a.store.ListJobs(c.query)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != c.count || len(page.Items) > 0 && page.Items[0]["id"] != c.first {
				t.Fatalf("unexpected results: %v", page.Items)
			}
		})
	}
	for _, query := range []model.JobQuery{{Status: "bad"}, {From: base.Add(time.Hour), To: base}, {From: base, To: base}} {
		_, err := a.store.ListJobs(query)
		requireActionFailure(t, err, model.InvalidInput)
	}
}
