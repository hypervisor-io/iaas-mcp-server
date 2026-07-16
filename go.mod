module github.com/hypervisor-io/iaas-mcp-server

// The MCP Go SDK's own go.mod floors at go 1.25.0 (v1.6.1), so that is our
// effective floor even though the plan's baseline is "Go 1.22+".
go 1.25.8

require (
	github.com/hypervisor-io/terraform-provider-iaas v0.2.3
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
