---
slug: /
id: intro
title: Introduction
sidebar_position: 1
---

# Kargus

**Kargus** drives Kubernetes RBAC from your identity provider's group
membership. You declare *who* a user is and *which IdP groups* they belong
to in a `UserAuthenticationBind` custom resource; the operator turns that into a
`ServiceAccount` plus the matching `RoleBinding`s and `ClusterRoleBinding`s.

The IdP is pluggable: any OIDC provider that reports groups (generic OIDC such as
Keycloak/Okta/Entra, or Google Workspace via the Directory API).

## How it works in one paragraph

Cluster operators tag the `Role`s and `ClusterRole`s they want to expose with an
annotation, `rbac.kargus.io/group: <group-id>`. When a `UserAuthenticationBind`
lands, the operator creates a `ServiceAccount` for the user, scans every
annotated role, and binds the user's ServiceAccount to each role whose group is
listed in the bind's `spec.memberships`. Membership changes re-sync; the bind
expires after its TTL.

## Repository layout

| Path | What it is |
| --- | --- |
| `operator/` | Kubebuilder operator: the `UserAuthenticationBind` API + controller |
| `service/` | Two binaries: the **broker** (OIDC, `cmd/server`) and the **proxy** (`cmd/proxy`) |
| `chart/` | Helm chart that installs the operator, broker, proxy, and CRD |
| `docs/` | This documentation site (Docusaurus) |

Three components:
- **operator** — reconciles `UserAuthenticationBind` CRs into a per-user
  ServiceAccount + RBAC bindings.
- **broker** — OIDC provider to your client, Relying Party to your IdP; on login
  it writes the CR, waits for the bind, and mints a ServiceAccount token.
- **proxy** — sits in front of the kube-apiserver, swaps the broker id_token for
  the user's SA token, so **no apiserver OIDC config is needed** (works on
  GKE/EKS/self-managed).

The default install namespace is `kargus-system` and the API group is `kargus.io`.

## Next steps

- [Architecture](./architecture.md) — components and the binding model
- [Reconcile flow](./reconcile-flow.md) — what happens on every reconcile
- [CRD reference](./crd-reference.md) — every field of `UserAuthenticationBind`
- [Broker service](./broker.md) — the OIDC login flow
- [Cluster auth proxy](./proxy.md) — how the SA token reaches the apiserver
- [Installation](./installation.md) — install with Helm
- [Helm chart](./helm.md) — chart values and trade-offs
- [Development](./development.md) — build, test, and contribute
