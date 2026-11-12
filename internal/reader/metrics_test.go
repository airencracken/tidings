package reader

import (
	"strings"
	"testing"
	"time"
)

func TestOwnerMetricsTotalsPermissionsAndFailure(t *testing.T) {
	h := webHarness(t)
	metrics, err := h.store.Metrics(t.Context(), time.Now())
	if err != nil || len(metrics) < 6 || metrics[0].Value != 3 {
		t.Fatal(metrics, err)
	}
	for _, path := range []string{"/admin/metrics", "/admin/metrics?owner=true&role=admin"} {
		w := h.request(1, "GET", path, nil)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "Instance overview") {
			t.Fatal(w.Code, w.Body.String())
		}
		for _, uid := range []int64{0, 2, 3} {
			w = h.request(uid, "GET", path, nil)
			if w.Code != 303 && w.Code != 403 {
				t.Fatal("metrics authorization", uid, w.Code)
			}
			if strings.Contains(w.Body.String(), "Instance overview") {
				t.Fatal("metrics leaked")
			}
		}
	}
	if _, err = h.store.DB.Exec("ALTER TABLE feeds RENAME TO unavailable_feeds"); err != nil {
		t.Fatal(err)
	}
	if w := h.request(1, "GET", "/admin/metrics", nil); w.Code != 500 || strings.Contains(w.Body.String(), "Instance overview") {
		t.Fatal("partial metrics published", w.Code)
	}
}
