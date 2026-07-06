<div align="center">

# iaas-mcp-server

**A remote, stateless Streamable-HTTP [MCP](https://modelcontextprotocol.io) server that exposes the Hypervisor.io cloud platform to AI agents.**

364 tools (302 user + 62 curated admin) over the platform REST API, backed by the
same tested API client as the OpenTofu provider.

</div>

---

`iaas-mcp-server` lets an AI agent (any MCP client - hosted connector, IDE
extension, or stdio bridge) manage real infrastructure on the Hypervisor.io platform: create and
manage instances, VPCs and subnets, Kubernetes clusters, managed databases, load
balancers, DNS, VPNs, object storage, volumes, autoscaling, and more. It speaks the
modern **Streamable-HTTP** MCP transport in **stateless** mode, so any request lands
on any replica and it scales horizontally with no session store.

It reuses the tested API client and async waiter from the OpenTofu provider
[`github.com/hypervisor-io/terraform-provider-iaas`](https://github.com/hypervisor-io/terraform-provider-iaas)
so the IaC provider and the MCP server call the platform API through one shared,
tested implementation. The API is the single source of truth; the provider and this
server are kept in lockstep with it by a CI gate (see [Tri-sync](#tri-sync)).

## Authentication

The server is a **Bearer pass-through**: it does not verify or store tokens. Every MCP
request must carry `Authorization: Bearer <platform-api-token>`; the server relays that
token to the platform API, which authorizes it. Token handling lives behind one seam
(`internal/iaasauth`), so a later OAuth 2.1 facade can replace it without touching any
tool.

Minting a token:

- **User token** (for `user.*` tools): create one in the platform panel under API
  tokens. By default it works from any IP; if you set `allowed_ips` on the token, the
  MCP server's egress IP must be in that list.
- **Admin token** (for `admin.*` tools): on the Master host run
  `php artisan api:admin-token generate`. Admin tokens are **IP-locked** to a
  registered IP, so the operator must register the MCP server's egress IP for that
  token. There is no client-side scope check: present a user token and `admin.*` tools
  return an authorization error with a hint; present an admin token and they work.

Never hard-code a token. The examples below read it from `$IAAS_API_TOKEN`.

## Running it

Requires Go 1.25+.

```bash
export IAAS_API_ENDPOINT="https://panel.example.com/api"   # required: platform API base URL
export IAAS_MCP_LISTEN=":8080"                             # optional (default shown)
export IAAS_API_TIMEOUT="30s"                              # optional (default shown)
export IAAS_API_INSECURE="false"                           # optional (default shown)

go build -o iaas-mcp-server .
./iaas-mcp-server
# or: go run .
```

Endpoints:

- `POST /mcp` - the Streamable-HTTP MCP endpoint. Requires `Authorization: Bearer`;
  a request without it gets an MCP auth error (HTTP 401).
- `GET /healthz` - a plain, unauthenticated liveness probe (returns `ok`).

The process shuts down gracefully on SIGINT/SIGTERM (draining in-flight requests).

### Environment variables

| Variable            | Required | Default | Meaning                                               |
|---------------------|----------|---------|-------------------------------------------------------|
| `IAAS_API_ENDPOINT` | yes      | -       | Base URL of the platform API (e.g. `https://panel.example.com/api`) |
| `IAAS_MCP_LISTEN`   | no       | `:8080` | Address the HTTP server binds to                      |
| `IAAS_API_TIMEOUT`  | no       | `30s`   | Per-request timeout to the platform API (Go duration) |
| `IAAS_API_INSECURE` | no       | `false` | Skip TLS verification on calls to the platform API (staging only) |

### Docker

```dockerfile
FROM golang:1.25 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /iaas-mcp-server .

FROM gcr.io/distroless/static
COPY --from=build /iaas-mcp-server /iaas-mcp-server
EXPOSE 8080
ENTRYPOINT ["/iaas-mcp-server"]
```

```bash
docker run --rm -p 8080:8080 -e IAAS_API_ENDPOINT="https://panel.example.com/api" iaas-mcp-server
```

## Tool surface

Tool names are `user.<family>.<action>` and `admin.<family>.<action>`. A grouped
sample (not exhaustive):

| Group | Example tools |
|-------|---------------|
| Instances | `user.instance.create`, `user.instance.list`, `user.instance.get`, `user.instance.delete` |
| VPC networking | `user.vpc.create`, `user.vpc.attach_instance`, `user.nat_gateway.create`, `user.instance_vpc.add_ip` |
| Firewall | `user.security_group.create`, `user.security_group.add_rule`, `user.ip_set.create`, `user.static_ip.allocate` |
| Kubernetes | `user.kubernetes_cluster.create`, `user.kubernetes_node_pool.create`, `user.kubernetes_cluster.kubeconfig`, `user.kubernetes_cluster.upgrade_workers` |
| Databases | `user.managed_database.create`, `user.managed_database.resize`, `user.db_replica.create`, `user.db_parameter_group.create` |
| Load balancers | `user.load_balancer.create`, `user.load_balancer.frontend_create`, `user.load_balancer.backend_create`, `user.load_balancer.target_create` |
| Storage | `user.volume.create`, `user.volume.snapshot_create`, `user.s3_bucket.create`, `user.s3_access_key.create` |
| DNS | `user.dns_zone.create`, `user.dns_record_set.create`, `user.dns_record.create`, `user.dns_zone.attach_vpc` |
| VPN | `user.vpn_gateway.create`, `user.vpn_gateway.add_peer`, `user.vpn_peering.create`, `user.vpn_gateway.peer_config` |
| Autoscaling | `user.autoscaling_group.create`, `user.autoscaling_policy.create`, `user.autoscaling_group.pause` |
| Compute add-ons | `user.image.create`, `user.docker_deployment.deploy_app`, `user.instance_backup_policy.create` |
| Account / misc | `user.ssh_key.create`, `user.user_script.create`, `user.project.create`, `user.notification_channel.create`, `user.alert_rule.create` |
| Catalog (read-only) | `user.catalog.locations`, `user.catalog.plans`, `user.catalog.images`, `user.catalog.k8s_versions` |
| Admin (curated) | `admin.instance.list`, `admin.hypervisor.list`, `admin.hypervisor.set_maintenance`, `admin.rdns_request.process` |

Cross-cutting behavior every tool inherits:

- **Confirm gate on destructive ops.** Delete/deallocate tools refuse unless the call
  passes `"confirm": true`, so an agent cannot destroy on a slip.
- **Idempotency keys.** Mutating tools accept an optional `idempotency_key`; where the
  platform endpoint supports it (e.g. Kubernetes create/update/delete) it is threaded
  through so a retried call is deduplicated server-side.
- **Async convergence.** Create tools that enqueue a task poll to a terminal state and
  return the converged object (e.g. `user.instance.create` waits until the instance is
  `deployed`; `user.kubernetes_cluster.create` waits until `running`).
- **Error mapping.** Platform `401/403` becomes a scope/IP-lock hint, `422` surfaces
  field errors, `404` becomes not-found, `429` is surfaced as retryable.

### Curated admin allowlist

`admin.*` is a deliberately narrow, safe allowlist (spec 17 decision D3): admin
**reads** (list/get/inspect/stats across instances, users, hypervisors, subnets, tasks,
plans, storages, etc.) plus two **reversible** safe mutations
(`admin.hypervisor.set_maintenance`, `admin.rdns_request.process`). Deliberately **not**
exposed: any billing/credit mutation, user deletion, impersonation-token minting,
hypervisor destroy/decommission, bulk IP/subnet deletion, and other irreversible or
fleet-wide-risk operations.

## Clients

See [`docs/CLIENTS.md`](docs/CLIENTS.md) for copy-paste configs (remote connector,
stdio bridge, generic `mcp.json`), a `curl` smoke test, and a first-task
walkthrough.

## Tri-sync

The platform API is the source of truth for three consumers that must stay in lockstep:
the API itself, the OpenTofu provider, and this MCP server. A checked-in
`api-manifest.json` records, per endpoint, whether the provider and MCP cover or
explicitly exclude it. Three CI checks (one per repo) fail the build on drift, and a
release cannot ship with an endpoint that this server neither covers nor excludes. This
repo's leg is `internal/tools/manifest_coverage_test.go` (it asserts every
`mcp.status=="covered"` endpoint names a tool this server actually registers), against
a vendored copy of the manifest refreshed with `make sync-manifest`. See spec 17
(`17-opentofu-mcp-api-trisync.md`) in the platform repo.

## Development

```bash
make build   # go build
make vet     # go vet
make fmt     # gofmt -w
make test    # go test ./...
make check   # all of the above
```

CI (`.github/workflows/ci.yml`) runs `go build`, `go vet`, `gofmt -l`, and `go test` on
every push and PR - no external services required.

## License

See [LICENSE](LICENSE).
