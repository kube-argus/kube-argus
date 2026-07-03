---
id: hardening
title: Security Hardening
sidebar_position: 11
---

# Security Hardening

Working ledger for the security posture of this repository, assessed against
**ISO/IEC 27001 Annex A** and **NIST CSF 2.0**. Sessions (human or agent)
should update the status tables below when items land — this file is the
source of truth for what has been done and what remains.

> Scope note: ISO 27001 certifies an organization's ISMS, not a repository.
> What the repo can evidence is mostly Annex A §8 (secure development,
> A.8.25–A.8.34) and slices of NIST CSF GOVERN / IDENTIFY / PROTECT.
> DETECT / RESPOND / RECOVER are runtime and organizational concerns.

<<<<<<< HEAD
**Position (last assessed 2026-07-03):** strong technical/product security,
=======
**Position (last assessed 2026-07-02):** strong technical/product security,
>>>>>>> feat/hardening-phase1-2
governance and supply-chain evidence partially closed. Details below.

## Safeguards in place

Verified by code review; re-verify if the referenced code moves.

| Area | Evidence | Mapping |
| --- | --- | --- |
| OAuth/OIDC hardening | PKCE S256 mandatory; exact `redirect_uri` match; `client_id`/`redirect_uri` validated before any redirect; broker-generated 256-bit state + IdP nonce verified; email-domain allowlist (`service/internal/broker/handlers.go`, `config.go`) | CSF PR.AA · A.8.5, A.5.15 |
| Token hygiene | Codes/state/refresh: 256-bit `crypto/rand`, one-time use (atomic Redis `GETDEL` — replay-safe in HA); refresh tokens rotate on every use; constant-time PKCE/secret compares; `Cache-Control: no-store` on token responses | CSF PR.AA-05 · A.8.5 |
| Short-lived credentials | SA tokens via TokenRequest with expiration; bind TTL; RS256 id_tokens; ephemeral dev signing key warns loudly | CSF PR.AA-05 · A.8.5 |
| Least-privilege RBAC | Broker: namespaced Role only. Operator ClusterRole uses the `bind` verb instead of holding granted roles (K8s escalation prevention) | CSF PR.AA · A.8.2, A.8.3 |
| Container hardening | Distroless static, nonroot 65532, `readOnlyRootFilesystem`, no privilege escalation, drop ALL caps, seccomp RuntimeDefault, resource limits | CSF PR.PS-01 · A.8.9 |
| Proxy safety | Strips `Impersonate-*` headers before forwarding; only broker id_tokens are swapped, everything else passes through so the apiserver stays the authority | CSF PR.AA · A.8.5 |
| Secret separation | Secrets vs ConfigMap split in chart; `existingSecret` support; no secrets committed | CSF PR.AA-06 · A.8.12 |
| CI hygiene | Actions pinned by SHA, `permissions: {}` default, `persist-credentials: false` | CSF GV.SC · A.8.28 |
| Logging | Structured JSON `slog`; request middleware; policy rejections logged with subject | CSF DE.CM · A.8.15 |
| TLS | In-process TLS option, cert-manager integration, proxy TLS by default | CSF PR.DS-02 · A.8.24 |

## Completed hardening items

| # | Item | Landed | Where |
| --- | --- | --- | --- |
| 1 | CI moved to repo root (was dead in `operator/.github/`): lint, operator + service tests, `govulncheck`, e2e | 2026-07-02 | `.github/workflows/ci.yml`, `test-e2e.yml` |
| 2 | Vulnerability disclosure policy | 2026-07-02 | `SECURITY.md` |
| 3 | Dependabot: gomod ×2, github-actions, docker (weekly) | 2026-07-02 | `.github/dependabot.yml` |
| 4 | Image tags default to chart `appVersion` (no more `latest`) | 2026-07-02 | `chart/values.yaml`, templates |
<<<<<<< HEAD
| 8 | Release workflow on tag `v*`: 3 multi-arch images (amd64+arm64) → ghcr, cosign keyless sign + GitHub provenance attestations, SPDX SBOMs attached to auto-created release, Helm chart pushed to `oci://ghcr.io/kube-argus/charts` + signed. Image naming reconciled: Makefile/Dockerfile now build `kargus-{operator,broker,proxy}` matching the chart | 2026-07-03 | `.github/workflows/release.yml`, `Makefile`, `Dockerfile`; process doc in `docs/docs/development.md` §Releases |

## Open items — execution plan

Phased by audit value per line of diff. Work top-down: Phase 1 → 2 → 3.
Update **Status** when work lands (open / in progress / done / deferred + reason).

### Phase 1 — revocation window (material finding)

| # | Item | Mapping | Status | Notes |
| --- | --- | --- | --- | --- |
| 5 | **Document the refresh-path revocation window** — group removal at the IdP takes effect on next fresh login; the refresh grant reuses cached groups for up to `BIND_TTL` (12h default). README "membership changes re-sync" is only true for fresh logins. Fix README wording + add a note in broker docs; mention shrinking via `BIND_TTL` / `REFRESH_TOKEN_LIFETIME_SECONDS` | A.5.18 · PR.AA-04 | open | Code: `handleRefresh` in `service/internal/broker/handlers.go` rebuilds identity from stored `RefreshGrant`, never re-queries IdP. ~15 lines of docs, no code |
| 6 | Re-resolve groups on refresh for `idpType=google` (Directory API supports it; generic OIDC cannot without login — documented window is the baseline there) | A.5.18 · PR.AA-04 | open | Optional if #5's documented window is accepted; ~40 lines + one test (refresh returns updated groups) |

=======
| 5 | Documented the refresh-path revocation window + TTL-tuning guidance (`BIND_TTL`, `REFRESH_TOKEN_LIFETIME_SECONDS`, `TOKEN_LIFETIME_SECONDS`, CR deletion for immediate revocation) | 2026-07-02 | `docs/docs/broker.md` ("Revocation window & TTL tuning"), `README.md`, `docs/docs/intro.md` |
| 7 | NetworkPolicy chart template, `networkPolicy.enabled=false` default — broker ingress from ingress-controller namespace (+ proxy JWKS), proxy ingress in-cluster, egress limited to DNS/IdP/apiserver/Redis | 2026-07-03 | `chart/templates/networkpolicy.yaml`, `chart/values.yaml`, `docs/docs/helm.md` |

## Open items — execution plan

Phased by audit value per line of diff. Work top-down: Phase 2 → 3.
Update **Status** when work lands (open / in progress / done / deferred + reason).

>>>>>>> feat/hardening-phase1-2
### Phase 2 — deployment hardening (chart)

| # | Item | Mapping | Status | Notes |
| --- | --- | --- | --- | --- |
<<<<<<< HEAD
| 7 | NetworkPolicy chart template, `enabled: false` default — broker holds IdP secrets and mints tokens, currently reachable from any pod. Broker ingress from ingress-controller namespace only; proxy ingress in-cluster; egress to IdP + apiserver | A.8.22 · PR.IR-01 | open | ~50 lines |
=======
>>>>>>> feat/hardening-phase1-2
| 10 | Secrets env → file mounts (env vars visible in `/proc`, crash dumps) | A.8.12 | deferred | Pod hardening already limits exposure; do only if an audit demands it. Touches `config.go` + chart |

### Phase 3 — release integrity

| # | Item | Mapping | Status | Notes |
| --- | --- | --- | --- | --- |
<<<<<<< HEAD
| 9 | Branch protection on `main`: require `lint`, `test-operator`, `test-service`, `govulncheck` checks + PRs | A.8.32 · GV.PO | open | Manual, after CI has run once on `main`: `gh api repos/kube-argus/kube-argus/branches/main/protection -X PUT` with `required_status_checks.contexts = ["lint","test-operator","test-service","govulncheck"]`, `strict: true`, PRs required (0 approvals — solo maintainer), or the same via GitHub settings UI |
=======
| 8 | Release workflow: on tag `v*` build 3 images via root Dockerfile targets, push ghcr, cosign keyless sign, SBOM (`anchore/sbom-action`); bump `Chart.yaml` version/appVersion in the tag commit | A.8.30, A.8.32 · GV.SC-04 | open | ~60 lines. Prereq: reconcile image naming — `chart/values.yaml` says `ghcr.io/kube-argus/kargus-*`, root `Makefile` says `github.com/kube-argus/kube-argus/*`; pick one, fix the other |
| 9 | Branch protection on `main`: require `lint`, `test-operator`, `test-service`, `govulncheck` checks + PRs | A.8.32 · GV.PO | open | Manual: GitHub settings or `gh api`, after CI has run once on `main` |
>>>>>>> feat/hardening-phase1-2

### Out of plan (considered, dismissed)

| # | Item | Mapping | Status | Notes |
| --- | --- | --- | --- | --- |
<<<<<<< HEAD
=======
| 6 | Re-resolve groups on refresh for `idpType=google` | A.5.18 · PR.AA-04 | dismissed | Would create provider-divergent revocation behavior (generic OIDC cannot re-resolve without a login) and put the Directory API on the refresh hot path. Uniform baseline instead: documented window + TTL tuning (item #5, `docs/docs/broker.md`) |
>>>>>>> feat/hardening-phase1-2
| 11 | Rate limiting on `/authorize` + `/token` | A.8.7 | deferred | All guessable artifacts are 256-bit one-time random values; volumetric protection belongs at the ingress |

## Accepted risks / known tradeoffs

- **Memory session store default** loses sessions on restart — documented dev
  default; production should set `sessionStore: redis` (CSF PR.IR-03).
<<<<<<< HEAD
- **Revocation window on refresh** (item #5/#6) — bounded by `BIND_TTL`;
  operators needing faster revocation lower the TTL or delete the
  `UserAuthenticationBind` CR directly.
- **No tag/Chart.yaml sync enforcement in the release workflow** —
  `chart/Chart.yaml` `version`/`appVersion` are a manual reference, bumped in
  the release PR before tagging (decided 2026-07-02). A tag pushed without the
  bump publishes images the chart's default tag does not point at; the fix is
  re-tagging after the bump, not a workflow guard.
=======
- **Revocation window on refresh** (items #5/#6) — bounded by `BIND_TTL`;
  operators needing faster revocation lower the TTLs or delete the
  `UserAuthenticationBind` CR directly. Documented with tuning guidance in
  `docs/docs/broker.md` ("Revocation window & TTL tuning").
>>>>>>> feat/hardening-phase1-2

## How to update this document

- Move landed items from their phase into *Completed* with the date and file
  paths; delete a phase heading once it is empty.
- New findings go into *Open* with an ISO 27001 / NIST CSF 2.0 mapping and a
  code reference so they can be re-verified.
- Consciously not-fixing something? Record it under *Accepted risks* with the
  reason — an undocumented gap is a finding, a documented one is a decision.
