package agent

import (
	"fmt"
	"testing"
	"time"

	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/storage"
)

func TestJobCursorPaginationWithTiesAndNewJobs(t *testing.T) {
	a := testAgent(t)
	when := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 235; i++ {
		id := fmt.Sprintf("job-%03d", i)
		putTestJob(t, a, &model.Job{ID: id, Status: "sent", CreatedAt: when})
	}
	seen := map[string]bool{}
	cursor := ""
	for n := 0; n < 3; n++ {
		p, err := a.store.ListJobs(model.JobQuery{Status: "sent", Before: cursor})
		if err != nil {
			t.Fatal(err)
		}
		expected := 100
		if n == 2 {
			expected = 35
		}
		if len(p.Items) != expected {
			t.Fatalf("page %d: %d", n, len(p.Items))
		}
		for _, j := range p.Items {
			if seen[j["id"].(string)] {
				t.Fatal("duplicate", j["id"].(string))
			}
			seen[j["id"].(string)] = true
		}
		cursor = p.Cursor
		if n == 0 {
			putTestJob(t, a, &model.Job{ID: "new-job", Status: "sent", CreatedAt: when.Add(time.Hour)})
		}
		if p.Counts["sent"] != p.Total {
			t.Fatal("counts differ")
		}
	}
	if len(seen) != 235 || cursor != "" || seen["new-job"] {
		t.Fatal("cursor lost original jobs")
	}
	for _, query := range []model.JobQuery{{Before: "garbage"}, {Limit: -1}, {Limit: storage.MaxJobPageSize + 1}} {
		_, err := a.store.ListJobs(query)
		requireActionFailure(t, err, model.InvalidInput)
	}
	p, err := a.store.ListJobs(model.JobQuery{Limit: 235})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 235 || p.Items[0]["id"] != "new-job" || p.Cursor == "" {
		t.Fatal("loaded window refresh failed")
	}
}
