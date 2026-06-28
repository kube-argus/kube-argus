---
id: crd-reference
title: CRD Reference
sidebar_position: 4
---

# CRD Reference — `UserAuthenticationBind`

**API group/version:** `kargus.io/v1` · **Kind:** `UserAuthenticationBind` ·
**Scope:** Namespaced

## Example

```yaml
apiVersion: kargus.io/v1
kind: UserAuthenticationBind
metadata:
  name: uid-0001
  namespace: kargus-system
spec:
  ttl: 12h
  domain: 
  user: lucas
  memberships:
    - gid: group/g12312312
      name: engineering
      domain: 
status:
  sv:
    ref: 7e3c...           # ServiceAccount UID
    status: binded
    expiresAt: 2026-06-28T12:00:00Z
  conditions:
    - type: Ready
      status: "True"
      reason: Bound
```

## `spec`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `ttl` | string | yes | Lifetime of the bind, e.g. `12h`, `30m`. Validated against `^[0-9]+(ns\|us\|µs\|ms\|s\|m\|h)$`. After it elapses the bind is unbound. |
| `domain` | string | yes | The IdP domain the user authenticates against (`hd` for Google, the email domain for generic OIDC). |
| `user` | string | yes | The user; also the name of the generated `ServiceAccount`. |
| `memberships` | `[]Membership` | no | Group memberships granted by this bind. |

### `Membership`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `gid` | string | yes | Group identifier from the IdP, e.g. `group/g12312312` (Google) or a `groups`-claim value (generic OIDC). **This is the value matched against the `rbac.kargus.io/group` annotation.** |
| `name` | string | yes | Human-readable group name. |
| `domain` | string | yes | Domain that owns the group. |

## `status`

| Field | Type | Description |
| --- | --- | --- |
| `sv.ref` | string | UID of the generated `ServiceAccount`. |
| `sv.status` | enum | One of `pending`, `binding`, `binded`, `unbound`, `failed`. |
| `sv.expiresAt` | time | When the bind expires (`renewed-at + ttl`; renewed on each login). |
| `conditions` | `[]Condition` | Standard conditions; `Ready` mirrors the phase. |

## The role annotation

Roles opt in to group-gating with an annotation. The operator only ever binds
roles that carry it:

```yaml
metadata:
  annotations:
    rbac.kargus.io/group: group/g12312312
```

A role is bound to a user when its annotation value equals one of the user's
`spec.memberships[].gid`.
