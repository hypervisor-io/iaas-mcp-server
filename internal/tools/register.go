package tools

import "github.com/modelcontextprotocol/go-sdk/mcp"

// toolRegistrars is the self-registration slice. Each tool-family file appends
// its registrar from an init(), so families can be added in parallel without
// any of them editing this shared file. RegisterAll runs them all.
var toolRegistrars []func(s *mcp.Server, deps Deps)

// RegisterAll registers every tool family on the server: the golden instance
// and VPC tools plus every self-registered family.
func RegisterAll(s *mcp.Server, deps Deps) {
	registerInstanceTools(s, deps)
	registerVPCTools(s, deps)
	for _, r := range toolRegistrars {
		r(s, deps)
	}
}

// RegisterGolden registers only the Phase 2 golden subset (instance + VPC). Kept
// for the golden-tools test, which asserts exactly that surface.
func RegisterGolden(s *mcp.Server, deps Deps) {
	registerInstanceTools(s, deps)
	registerVPCTools(s, deps)
}
