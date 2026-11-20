package tools_test

import (
	"net/http"
	"strings"
	"testing"
)

// API-422: a refused field on these routes is a 422 with message and errors keyed by field (it used to be a 400
// or a 200 success:false). The tool must surface it as an error that names the field.
func TestRefusedFieldIs422AndSurfacesTheField(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vpc/{vpcId}/nat-gateway/{id}/subnet", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"success": false, "message": "Only private subnets can be attached",
			"errors": map[string]any{"subnet_id": []string{"Only private subnets can be attached"}},
		})
	})
	mux.HandleFunc("POST /user-scripts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"message": "The name field is required.",
			"errors":  map[string]any{"name": []string{"The name field is required."}},
		})
	})
	cs := connectSession(t, mux)

	res := callTool(t, cs, "user.nat_gateway.attach_subnet", map[string]any{"vpc_id": "vpc-1", "id": "nat-1", "subnet_id": "sub-pub"})
	if !res.IsError || !strings.Contains(resultText(t, res), "subnet_id") {
		t.Fatalf("attach_subnet: want an error naming subnet_id, got %s", resultText(t, res))
	}
	res = callTool(t, cs, "user.user_script.create", map[string]any{"name": "", "type": "bash", "content": "true"})
	if !res.IsError || !strings.Contains(resultText(t, res), "name") {
		t.Fatalf("user_script.create: want an error naming name, got %s", resultText(t, res))
	}
}
