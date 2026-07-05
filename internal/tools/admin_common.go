package tools

// Shared types and helpers for the admin.* tool families (spec 17 decision D3:
// a curated safe allowlist over the admin API). Every admin tool sets
// Spec.Admin=true so a 401/403 is enriched into an admin-scope/IP-lock hint;
// auth itself is unchanged (the same Bearer pass-through seam - the admin API
// authorizes the admin-scoped token server-side).

// AdminListResult is the shared output for admin list/stats-collection reads.
type AdminListResult struct {
	Items []map[string]any `json:"items"`
	Count int              `json:"count"`
}

// AdminItemResult is the shared output for admin single-object reads (which
// return the bare model) and for the safe mutations' response envelopes.
type AdminItemResult struct {
	Item map[string]any `json:"item"`
}

// adminList wraps a client list call into an AdminListResult.
func adminList(items []map[string]any, err error) (AdminListResult, error) {
	if err != nil {
		return AdminListResult{}, err
	}
	return AdminListResult{Items: items, Count: len(items)}, nil
}

// adminItem wraps a client single-object call into an AdminItemResult.
func adminItem(obj map[string]any, err error) (AdminItemResult, error) {
	if err != nil {
		return AdminItemResult{}, err
	}
	return AdminItemResult{Item: obj}, nil
}

// AdminIDInput is the common {id} input for admin get/stats-by-id reads.
type AdminIDInput struct {
	ID string `json:"id" jsonschema:"UUID of the resource"`
}

// AdminUserIDInput identifies a user for admin user-scoped reads.
type AdminUserIDInput struct {
	UserID string `json:"user_id" jsonschema:"UUID of the user"`
}
