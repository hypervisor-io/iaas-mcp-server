# Connecting a client to iaas-mcp-server

`iaas-mcp-server` is a **remote, stateless Streamable-HTTP MCP server**. Every request
must carry `Authorization: Bearer <platform-api-token>` (see
[Authentication](../README.md#authentication)). These examples assume the server is
reachable at `https://mcp.example.com/mcp` and your token is in `$IAAS_API_TOKEN`.
Never paste a real token into a config you commit.

## Remote connector (hosted MCP client)

In any MCP client that supports remote (Streamable-HTTP) connectors, add a custom
connector pointing at the server's `/mcp` URL and supply the Authorization header:

- **URL:** `https://mcp.example.com/mcp`
- **Header:** `Authorization: Bearer <your platform API token>`

The connector then lists the tools and lets the agent call them. Use a **user** token for
`user.*` tools, or an **admin** token (IP-locked; the server's egress IP must be
registered) to also reach `admin.*` tools.

## Stdio-only client (via the mcp-remote bridge)

Some MCP clients launch servers only as local stdio processes. Bridge them to the remote
Streamable-HTTP endpoint with `mcp-remote`. In the client's MCP server config (a stdio
server entry):

```json
{
  "mcpServers": {
    "hypervisor": {
      "command": "npx",
      "args": [
        "-y", "mcp-remote",
        "https://mcp.example.com/mcp",
        "--header", "Authorization: Bearer ${IAAS_API_TOKEN}"
      ],
      "env": { "IAAS_API_TOKEN": "your-platform-api-token" }
    }
  }
}
```

## Generic MCP client (mcp.json-style)

Clients that speak Streamable-HTTP directly (e.g. VS Code, custom agents) take the URL
and header inline:

```json
{
  "servers": {
    "hypervisor": {
      "type": "http",
      "url": "https://mcp.example.com/mcp",
      "headers": { "Authorization": "Bearer ${env:IAAS_API_TOKEN}" }
    }
  }
}
```

## curl smoke test

The transport is JSON-RPC over HTTP; responses come back as Server-Sent Events, so send
`Accept: application/json, text/event-stream`. Because the server is stateless, each
request is independent.

Initialize:

```bash
curl -sN https://mcp.example.com/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Authorization: Bearer $IAAS_API_TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize",
       "params":{"protocolVersion":"2025-06-18","capabilities":{},
                 "clientInfo":{"name":"smoke","version":"0.0.1"}}}'
```

List the tools:

```bash
curl -sN https://mcp.example.com/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Authorization: Bearer $IAAS_API_TOKEN" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
```

Liveness (no auth):

```bash
curl https://mcp.example.com/healthz    # -> ok
```

A `POST /mcp` without an `Authorization` header returns HTTP 401 (an MCP auth error).

## Your first task

A typical flow: discover what to deploy, create an instance (async), then attach it to a
VPC. Tool calls use `tools/call` with `{"name": ..., "arguments": ...}`.

**1. Discover a location, plan, and image** with the read-only catalog tools:

- `user.catalog.locations` -> pick a `location_id`
- `user.catalog.plan_groups` `{ "location_id": "<loc>" }` -> pick a `plan_group_id`
- `user.catalog.plans` `{ "location_id": "<loc>", "plan_group_id": "<pg>" }` -> pick a `plan_id`
- `user.catalog.images` `{}` -> pick an `image_id`

**2. See what you already have:**

- `user.instance.list` `{}`

**3. Create an instance** (this waits until the instance is `deployed`, then returns it):

- `user.instance.create`
  ```json
  { "location_id": "<loc>", "plan_id": "<plan>", "image_id": "<image>", "hostname": "web-1" }
  ```
  Optional args: `vpc_id` + `vpc_subnet_id` (attach at create), `ssh_keys` (UUIDs),
  `timezone`, `cloudcfg`.

**4. Attach the instance to a VPC subnet:**

- `user.vpc.attach_instance`
  ```json
  { "instance_id": "<instance id from step 3>", "vpc_id": "<vpc>", "vpc_subnet_id": "<subnet>" }
  ```
  The platform auto-assigns the lowest free IP as primary and returns the attached IPs.

**5. Clean up (destructive - requires confirm):**

- `user.instance.delete`
  ```json
  { "id": "<instance id>", "confirm": true }
  ```
  Without `"confirm": true` the tool refuses. It then waits until the instance is fully
  removed before returning.

That is the whole pattern: read tools to discover ids, a create tool that converges
asynchronously, action tools keyed by the resource id, and a confirm-gated delete. Every
other family (`user.kubernetes_cluster.*`, `user.managed_database.*`,
`user.load_balancer.*`, `user.dns_zone.*`, ...) follows the same shape.
