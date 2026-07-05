package tools

import "github.com/modelcontextprotocol/go-sdk/mcp"

// RegisterGolden registers the Phase 2 golden tools on the server: the four
// instance tools and the VPC tools. Later phases add more registrar calls here
// (or their own registrars) as the tool surface fans out.
func RegisterGolden(s *mcp.Server, deps Deps) {
	registerInstanceTools(s, deps)
	registerVPCTools(s, deps)
}
