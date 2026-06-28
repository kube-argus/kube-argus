---
id: development
title: Development
sidebar_position: 8
---

# Development

The repository is two Go modules plus a Helm chart:

| Module / path | Go module path |
| --- | --- |
| `operator/` | `git./kargos/operator` |
| `service/` | `git./kargos/service` (replaces operator locally) |
| `chart/` | Helm chart (no Go) |

A root `go.work` ties both modules together. The repo root is not itself a
module, so build by module path from the root:

```bash
go build git./kargos/operator/... git./kargos/service/...
```

Or `cd` into a module and use `./...` as usual. The `replace` in `service/go.mod`
stays — it is what the Docker build (which has no `go.work`) relies on.

## Operator

From `operator/`:

```bash
make manifests generate   # regenerate CRDs/RBAC and DeepCopy after editing types
make build                # compile the manager
make run                  # run locally against your kubeconfig context
```

### Tests

```bash
go test ./internal/controller/...        # fast unit tests (fake client, no envtest)
make test                                # full suite incl. envtest (downloads binaries)
```

Unit tests in `internal/controller/reconcile_test.go` use the controller-runtime
**fake client**, so they need no external binaries. They cover the whole
reconcile: finalizer, pending→binded progression, ServiceAccount creation,
ClusterRole/Role matching, stale-binding prune, invalid TTL, expiry/unbind, and
finalizer cleanup on delete.

The Ginkgo suite (`suite_test.go`) uses **envtest** (a real API server + etcd) and
requires `make setup-envtest` first.

### Where things live

| Path | Purpose |
| --- | --- |
| `api/v1/userauthenticationbind_types.go` | CRD schema + kubebuilder markers |
| `internal/controller/userauthenticationbind_controller.go` | reconcile logic |
| `config/crd/bases/` | generated CRD (do not edit by hand) |
| `config/rbac/role.yaml` | generated RBAC (do not edit by hand) |

After editing `*_types.go` or any `+kubebuilder` marker, always run
`make manifests generate`. The chart's CRD copy in `chart/crds/` must be
re-copied from `operator/config/crd/bases/` when the schema changes.

## Broker + proxy (service)

The `service/` module builds two binaries — the broker (`cmd/server`) and the
[cluster auth proxy](./proxy.md) (`cmd/proxy`):

```bash
go test ./...        # unit tests: fake client + httptest, no cluster needed
go build ./cmd/server
go build ./cmd/proxy
```

Tests cover the store (one-shot/expiry + refresh rotation), config validation,
CR upsert/wait, the id_token signer, the proxy token cache, and the full
authorize → callback → token flow (PKCE + replay rejection). The Docker image
contains both binaries; the proxy Deployment overrides `command: ["/proxy"]`.

## Images (root Makefile)

Build/push the three deployable bundles from the repo root:

```bash
make build  BUNDLE=operator|service|proxy REPOSITORY=<registry/org> VERSION=<tag>
make push   BUNDLE=...                     REPOSITORY=...            VERSION=...
make release BUNDLE=...                    # build + push
make release-all                           # all three bundles
make image  BUNDLE=service                 # print the resolved image ref
```

Image name is `<REPOSITORY>/<BUNDLE>:<VERSION>`. `service` and `proxy` come from
the root `Dockerfile` (both binaries; `--build-arg BIN` selects the entrypoint);
`operator` from `operator/Dockerfile`. Point the chart's `*.image.repository`
values at the names you push.

## Helm chart

From the repo root:

```bash
helm lint chart/
helm template kg chart/ -n kargus-system            # render with defaults
helm template kg chart/ -n kargus-system \
  --set redis.enabled=true --set broker.config.sessionStore=redis
```

## Documentation site

This site is built with [Docusaurus](https://docusaurus.io). From `docs/`:

```bash
npm install
npm start            # live dev server at http://localhost:3000
npm run build        # static build into docs/build
npm run serve        # serve the production build
```

Docs are Markdown under `docs/docs/`; the sidebar order is defined in
`docs/sidebars.js`.
