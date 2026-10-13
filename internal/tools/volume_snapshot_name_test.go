package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func TestVolumeSnapshotToolNameRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name    string
		invalid bool
	}{
		{"", true}, {"   ", true}, {" nightly ", true}, {"nightly\t", true}, {"\nnightly", true}, {"0", true},
		{"nightly", false}, {"00", false}, {"nightly copy", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			gets, posts := 0, 0
			var snapshots []any
			cs := connectSession(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch r.Method {
				case http.MethodGet:
					gets++
					writeJSON(w, http.StatusOK, map[string]any{"volume": map[string]any{"id": "vol-1", "snapshots": snapshots}})
				case http.MethodPost:
					posts++
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					name, _ := body["name"].(string)
					if name != tc.name {
						t.Errorf("request changed name %q to %q", tc.name, name)
					}
					storedName := strings.TrimSpace(name)
					if storedName == "" || storedName == "0" {
						storedName = "snap-generated"
					}
					snapshots = append(snapshots, map[string]any{"id": "snap-1", "name": storedName, "status": "available"})
					// An uncertain recorded response keeps the old implementation's invalid-name
					// witness bounded: it performs one read-back instead of a long resolution wait.
					if tc.invalid {
						writeJSON(w, http.StatusServiceUnavailable, map[string]any{"message": "Service Unavailable"})
						return
					}
					writeJSON(w, http.StatusOK, map[string]any{"success": true, "queue": map[string]any{"id": "q-1"}})
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
			}))
			res := callTool(t, cs, "user.volume.snapshot_create", map[string]any{"volume_id": "vol-1", "name": tc.name})
			if tc.invalid {
				text := resultText(t, res)
				if !res.IsError || !strings.Contains(text, "snapshot name") || strings.Contains(text, "no definite answer") {
					t.Errorf("name=%q result=%q; want a name validation error before creation", tc.name, text)
				}
				mu.Lock()
				defer mu.Unlock()
				if gets != 0 || posts != 0 || len(snapshots) != 0 || res.StructuredContent != nil {
					t.Fatalf("name=%q reads=%d sends=%d leftovers=%d result=%v; want zero requests, no snapshot or adopted id", tc.name, gets, posts, len(snapshots), res)
				}
			} else {
				var result tools.SnapshotResult
				unmarshalResult(t, res, &result)
				if result.Snapshot["id"] != "snap-1" || result.Snapshot["name"] != tc.name {
					t.Fatalf("name=%q result=%v; want original name and new id", tc.name, result)
				}
				mu.Lock()
				defer mu.Unlock()
				if posts != 1 || len(snapshots) != 1 {
					t.Fatalf("sends=%d snapshots=%d; want one", posts, len(snapshots))
				}
			}
		})
	}
}
