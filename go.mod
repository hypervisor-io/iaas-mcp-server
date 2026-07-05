module github.com/hypervisor-io/iaas-mcp-server

// The MCP Go SDK's own go.mod floors at go 1.25.0 (v1.6.1), so that is our
// effective floor even though the plan's baseline is "Go 1.22+".
go 1.25.8

require (
	github.com/hypervisor-io/terraform-provider-iaas v0.0.0
	github.com/modelcontextprotocol/go-sdk v1.6.1
)

require (
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
)

// Local sibling checkout, per docs/plans/2026-07-06-mcp-server-build.md Phase 1:
// the client/waiter packages are promoted, importable packages in that repo
// (not yet a separately tagged module - see spec 17 Phase 0 D2 options).
replace github.com/hypervisor-io/terraform-provider-iaas => ../terraform-provider-iaas
