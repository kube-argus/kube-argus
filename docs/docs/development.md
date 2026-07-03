---
id: development
title: Development
sidebar_position: 10
---

# Development

The repository is two Go modules plus a Helm chart:

| Module / path | Go module path |
| --- | --- |
| `operator/` | `github.com/kube-argus/kube-argus/operator` |
| `service/` | `github.com/kube-argus/kube-argus/service` (replaces operator locally) |
| `chart/` | Helm chart (no Go) |

A root `go.work` ties both modules together. The repo root is not itself a
module, so build by module path from the root:

```bash
go build github.com/kube-argus/kube-argus/operator/... github.com/kube-argus/kube-argus/service/...
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
make build  BUNDLE=operator|broker|proxy REPOSITORY=<registry/org> VERSION=<tag>
make push   BUNDLE=...                    REPOSITORY=...            VERSION=...
make release BUNDLE=...                   # build + push (omit BUNDLE for all three)
make image                                # print the resolved image ref(s)
```

Image name is `<REPOSITORY>/kargus-<BUNDLE>:<VERSION>` — the same names the
chart's `*.image.repository` values default to. All three images are `--target`
stages in the single root `Dockerfile`; the `broker` image contains both the
broker and proxy binaries (the proxy Deployment overrides `command: ["/proxy"]`).

## Releases

Pushing a tag `v*` runs `.github/workflows/release.yml`, which:

1. builds the three images for `linux/amd64` + `linux/arm64` and pushes them to
   `ghcr.io/kube-argus/kargus-{operator,broker,proxy}:<tag>`;
2. signs each image digest with cosign (keyless, verifiable via the public
   Rekor log) and attaches GitHub build-provenance attestations;
3. generates an SPDX SBOM per image and attaches them to an auto-created
   GitHub release;
4. packages the Helm chart, pushes it to `oci://ghcr.io/kube-argus/charts`,
   and cosign-signs it.

The chart is published with whatever `version`/`appVersion` `chart/Chart.yaml`
carries — bump both in the release PR **before** tagging, or the chart's
default image tag will point at the previous release.

Verify a released image:

```bash
cosign verify ghcr.io/kube-argus/kargus-broker:<tag> \
  --certificate-identity-regexp 'github.com/kube-argus/kube-argus' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

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
