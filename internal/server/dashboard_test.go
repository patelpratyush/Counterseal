package server

import (
	"net/http"
	"testing"
)

func TestDashboardOverview(t *testing.T) {
	f := setup(t)
	empty := f.must(t, "GET", "/v1/dashboard/overview", nil, 200)
	if len(empty["runs"].([]any)) != 0 || empty["total"].(float64) != 0 {
		t.Fatal(empty)
	}
	var blockedRun string
	for i := 0; i < 22; i++ {
		id, run := f.root(t)
		if i == 0 {
			blockedRun = run
			f.must(t, "POST", "/v1/evaluate/action", actionBody(id), 403)
		}
	}
	first := f.must(t, "GET", "/v1/dashboard/overview", nil, 200)
	second := f.must(t, "GET", "/v1/dashboard/overview?page=2", nil, 200)
	if first["total"].(float64) != 22 || len(first["runs"].([]any)) != 20 || len(second["runs"].([]any)) != 2 {
		t.Fatal(first, second)
	}
	ids := map[string]bool{}
	for _, page := range []map[string]any{first, second} {
		for _, raw := range page["runs"].([]any) {
			id := raw.(map[string]any)["id"].(string)
			if ids[id] {
				t.Fatal("duplicate across pages")
			}
			ids[id] = true
		}
	}
	blocked := f.must(t, "GET", "/v1/dashboard/overview?filter=blocked", nil, 200)
	if blocked["total"].(float64) != 1 || blocked["runs"].([]any)[0].(map[string]any)["id"] != blockedRun {
		t.Fatal(blocked)
	}
	stats := first["stats"].(map[string]any)
	if stats["blocked_actions"].(float64) != 1 || stats["policy_versions"].(float64) != 1 {
		t.Fatal(stats)
	}
	match := f.must(t, "GET", "/v1/dashboard/overview?q="+blockedRun, nil, 200)
	if match["total"].(float64) != 1 {
		t.Fatal(match)
	}
	percent := f.must(t, "GET", "/v1/dashboard/overview?q=%25", nil, 200)
	if percent["total"].(float64) != 0 {
		t.Fatal("wildcard interpreted as search", percent)
	}
	for _, path := range []string{"?page=0", "?page=no", "?page=100001", "?filter=invalid"} {
		f.must(t, "GET", "/v1/dashboard/overview"+path, nil, 400)
	}
	response, err := http.Get(f.http.URL + "/v1/dashboard/overview")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("overview is not authenticated")
	}
}
