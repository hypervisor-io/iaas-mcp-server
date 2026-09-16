package tools

// location_id is the public name of a hypervisor group on every user-facing
// MCP tool (mirrors the user API contract). Tool inputs use location_id;
// hypervisor_group_id remains accepted as a deprecated input alias for one
// release, and location_id wins when both keys arrive. Tool outputs are the
// API's own objects, which carry location_id only.

// locationIDAliasNote is appended to the description of every tool whose input
// still accepts the deprecated hypervisor_group_id alias (one release).
const locationIDAliasNote = ` Accepts the deprecated "hypervisor_group_id" as an alias for "location_id"; "location_id" wins when both are sent.`

// resolveLocationID applies that input contract: it returns the value to send
// to the API as location_id, preferring the canonical key over the alias.
func resolveLocationID(locationID, hypervisorGroupID string) string {
	if locationID != "" {
		return locationID
	}
	return hypervisorGroupID
}
