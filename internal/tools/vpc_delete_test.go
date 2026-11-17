package tools_test

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// vpcLifecycleMock answers DELETE /vpc/{id} with 409 pending, then 409
// incomplete, then 200, and refuses every NAT gateway create with vpc_deleting.
func vpcLifecycleMock(deletes *int32) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /vpc/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch atomic.AddInt32(deletes, 1) {
		case 1:
			writeJSON(w, http.StatusConflict, map[string]any{
				"success": false, "reason": "vpc_delete_pending", "message": "A NAT gateway is still being removed",
				"pending_nat_gateways": []string{"gw-1", "gw-2"},
			})
		case 2:
			writeJSON(w, http.StatusConflict, map[string]any{
				"success": false, "reason": "vpc_delete_incomplete", "message": "A later step failed",
			})
		default:
			writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "VPC deleted"})
		}
	})
	mux.HandleFunc("POST /vpc/{vpcId}/nat-gateway", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"success": false, "reason": "vpc_deleting", "message": "The VPC is being deleted",
		})
	})
	return mux
}

func TestVPCDelete_pendingAndIncompleteAreStructuredResults(t *testing.T) {
	var deletes int32
	cs := connectSession(t, vpcLifecycleMock(&deletes))
	args := map[string]any{"id": "vpc-1", "confirm": true}

	res := callTool(t, cs, "user.vpc.delete", args)
	if res.IsError {
		t.Fatalf("a pending delete must be a result, not an error: %s", resultText(t, res))
	}
	var got tools.DeleteVPCResult
	unmarshalResult(t, res, &got)
	if got.Deleted || !got.Pending || got.Reason != "vpc_delete_pending" || got.ID != "vpc-1" {
		t.Fatalf("pending result = %+v", got)
	}
	if len(got.PendingNatGateways) != 2 || got.PendingNatGateways[0] != "gw-1" {
		t.Errorf("pending gateways = %v", got.PendingNatGateways)
	}
	if !strings.Contains(got.Message, "again") || !strings.Contains(got.Message, "deleting") {
		t.Errorf("message does not say to repeat the delete: %q", got.Message)
	}

	res = callTool(t, cs, "user.vpc.delete", args)
	got = tools.DeleteVPCResult{}
	unmarshalResult(t, res, &got)
	if !got.Pending || got.Reason != "vpc_delete_incomplete" || len(got.PendingNatGateways) != 0 {
		t.Fatalf("incomplete result = %+v", got)
	}

	res = callTool(t, cs, "user.vpc.delete", args)
	got = tools.DeleteVPCResult{}
	unmarshalResult(t, res, &got)
	if !got.Deleted || got.Pending || got.Reason != "" {
		t.Fatalf("final result = %+v", got)
	}
}

func TestVPCDelete_otherFailureStaysAnError(t *testing.T) {
	cs := connectSession(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": "Subnet still has used IPs"})
	}))
	res := callTool(t, cs, "user.vpc.delete", map[string]any{"id": "vpc-1", "confirm": true})
	if !res.IsError || !strings.Contains(resultText(t, res), "Subnet still has used IPs") {
		t.Fatalf("expected the refusal as an error, got %s", resultText(t, res))
	}
}

func TestCreateInDeletingVPC_isTypedRefusal(t *testing.T) {
	var deletes int32
	cs := connectSession(t, vpcLifecycleMock(&deletes))
	res := callTool(t, cs, "user.nat_gateway.create", map[string]any{"vpc_id": "vpc-1", "name": "gw"})
	if !res.IsError {
		t.Fatal("create in a deleting VPC must be an error result")
	}
	txt := resultText(t, res)
	for _, want := range []string{"refused (vpc_deleting)", "retrying will not help", "reason: vpc_deleting"} {
		if !strings.Contains(txt, want) {
			t.Errorf("error %q lacks %q", txt, want)
		}
	}
}
