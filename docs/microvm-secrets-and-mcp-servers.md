# Sandbox secrets and MCP servers

`user.microvm_secret.list` defaults to environment secrets. Supply `{"kind":"all"}` to include registry credentials. Create takes `name` and a write-only `value`; for `kind:"registry"`, also supply `registry` and `username`. Update accepts a name and/or new value. Omitting value preserves the stored value. An empty value is invalid. The kind cannot change, and registry changes are refused while a kit references the credential. Delete requires `confirm:true`. Existing sandboxes keep their original environment after rotation or deletion.

Secret values are never returned. Credential-bearing request failures retain status and machine reason, but discard potentially reflective error details. A rejected request can therefore have a generic error message. Check the documented fields and reason rather than relying on reflected credential text.

Use `user.microvm_mcp_server.providers` to inspect supported providers, then `create` with `provider`, `name`, and `label`. A custom server also needs HTTPS `url` and `auth_kind` (`oauth`, `bearer`, or `none`). Bearer servers require `static_token`. Optional `enabled:false` is respected. Custom `headers` must contain only nonsecret headers, at most eight; the service limits each value to 1024 bytes and rejects credential header names.

For example, an OAuth server can be created without placing a credential in the request:

```json
{"provider":"linear","name":"linear","label":"Linear","auth_kind":"oauth"}
```

`list` and `get` return metadata with the current `revision`, your own `connection`, `credential_state`, `probe`, and binding summary. Empty `revision.headers` and empty catalog `scope_presets` can be arrays in the Master response. The server belongs to the active account, while OAuth connections belong to the API token's acting person.

`connect` accepts the server `id` and optional `scope_preset`. Open its returned `authorize_url` in a browser session signed in as that same person. The tool starts authorization; it cannot complete browser consent. `disconnect` requires `confirm:true` and removes only that person's OAuth connection, leaving other people and static account credentials unchanged.

`update` accepts label, enabled, URL, auth kind, headers, and a token only when switching into bearer authentication. Send `headers:{}` to clear custom headers. Name and provider are immutable. Identity edits and deletion may return `409 mcp_server_in_use` while active sandbox bindings exist.

`rotate_token` takes `id`, the new `static_token`, and `confirm:true`. It updates an existing bearer credential without creating a new destination revision. The token is supplied by the caller; neither create nor rotation returns a token. Delete also requires `confirm:true`.

Writes are sent once. A timeout or server failure is not proof that a write did not happen: inspect `list` or `get` before repeating a create or rotation.

OpenTofu resources are `iaas_microvm_secret` and `iaas_microvm_mcp_server`. The latter uses `provider_slug` for the API's `provider` field because `provider` is a Terraform meta-argument. Both use write-only sensitive credential arguments with nonsecret version counters: increment `value_version` or `static_token_version` to rotate. The CLI must support write-only arguments. Import recovers metadata only. OAuth connect and disconnect remain separate browser/person actions, outside declarative provider management.
