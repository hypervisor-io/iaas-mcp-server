package tools_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func dnsMock() http.Handler {
	mux := http.NewServeMux()
	var mu sync.Mutex
	zoneDeleted := false

	mux.HandleFunc("POST /dns-zones", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "zone": map[string]any{"id": "z-1", "name": "example.com"}})
	})
	mux.HandleFunc("GET /dns-zones", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "z-1", "name": "example.com"}},
		})
	})
	mux.HandleFunc("GET /dns-zone/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gone := zoneDeleted
		mu.Unlock()
		if gone {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Zone not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"zone":    map[string]any{"id": r.PathValue("id"), "name": "example.com", "vpcs": []any{}},
		})
	})
	mux.HandleFunc("DELETE /dns-zone/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		zoneDeleted = true
		mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "queued"})
	})
	mux.HandleFunc("POST /dns-zone/{id}/attach-vpc", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "attached"})
	})
	mux.HandleFunc("POST /dns-zone/{id}/record-sets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "record_set": map[string]any{"id": "rs-1", "name": "www", "type": "A"}})
	})
	mux.HandleFunc("POST /dns-zone/{id}/record-set/{rs}/records", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "record": map[string]any{"id": "rec-1", "value": "203.0.113.1"}})
	})
	mux.HandleFunc("DELETE /dns-zone/{id}/record-set/{rs}/record/{rec}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})
	return mux
}

func TestDNS_ZoneRecordSetRecord(t *testing.T) {
	cs := connectSession(t, dnsMock())

	res := callTool(t, cs, "user.dns_zone.create", map[string]any{"name": "example.com"})
	var zone tools.DnsZoneResult
	unmarshalResult(t, res, &zone)
	if zone.Zone["id"] != "z-1" {
		t.Fatalf("zone create id = %v, want z-1", zone.Zone["id"])
	}

	res = callTool(t, cs, "user.dns_zone.list", map[string]any{})
	var zl tools.DnsZoneListResult
	unmarshalResult(t, res, &zl)
	if zl.Count != 1 {
		t.Errorf("zone list count = %d, want 1", zl.Count)
	}

	res = callTool(t, cs, "user.dns_zone.attach_vpc", map[string]any{"zone_id": "z-1", "vpc_id": "vpc-1"})
	var ok tools.OKResult
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Errorf("attach_vpc ok = false")
	}

	res = callTool(t, cs, "user.dns_record_set.create", map[string]any{
		"zone_id": "z-1", "name": "www", "type": "A", "routing_policy": "simple", "ttl": 300,
	})
	var rs tools.DnsRecordSetResult
	unmarshalResult(t, res, &rs)
	if rs.RecordSet["id"] != "rs-1" {
		t.Fatalf("record_set create id = %v, want rs-1", rs.RecordSet["id"])
	}

	res = callTool(t, cs, "user.dns_record.create", map[string]any{
		"zone_id": "z-1", "record_set_id": "rs-1", "value": "203.0.113.1",
	})
	var rec tools.DnsRecordResult
	unmarshalResult(t, res, &rec)
	if rec.Record["id"] != "rec-1" {
		t.Fatalf("record create id = %v, want rec-1", rec.Record["id"])
	}
}

func TestDNS_ZoneDeleteConfirmConverges(t *testing.T) {
	cs := connectSession(t, dnsMock())
	res := callTool(t, cs, "user.dns_zone.delete", map[string]any{"id": "z-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.dns_zone.delete", map[string]any{"id": "z-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed zone delete did not converge")
	}
}
