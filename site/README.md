# Documentation site

`index.html` is a single, self-contained documentation page for
`iaas-mcp-server` (no external CSS/JS/fonts/CDN, works offline). It covers what
the server is (remote, stateless Streamable-HTTP MCP), Bearer pass-through auth
and its environment variables, quick-start client configs (remote connector,
mcp-remote stdio bridge, generic `mcp.json`, `curl` smoke test), the full tool
surface with search, the cross-cutting behavior every tool inherits
(confirm-gate, idempotency keys, async convergence, error mapping, the curated
admin allowlist), and the tri-sync CI gate.

It is a plain static file: no build step, no dependencies, nothing to install.

## Hosting on GitHub Pages

This repo ships a Pages workflow at `.github/workflows/pages.yml` that deploys
this `site/` folder automatically.

One-time setup:

1. Repo Settings, then Pages.
2. Under "Build and deployment", set Source to "GitHub Actions".

After that, every push to `main` that touches `site/` (or the workflow) publishes
the page. You can also trigger it manually from the Actions tab
("Deploy documentation site to GitHub Pages", then Run workflow). The published
URL appears in the workflow run and under Settings, then Pages, for example
`https://hypervisor-io.github.io/iaas-mcp-server/`.

Note: the repo must be public (or on a plan that allows private Pages) for the
deployment to serve. GitHub Pages branch deployment only serves from the repo
root or `/docs`, which is why this uses the Actions workflow to publish the
`site/` folder directly.

## Updating the page

Edit `index.html` and push. There is no build step; it is one HTML file. The
headline tool counts (380 total, 309 user, 71 admin) and the tool families
mirror `internal/tools/`; keep them in step when the tool surface changes.
