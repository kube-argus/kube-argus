---
id: idp-google
title: IdP — Google Workspace
sidebar_position: 6
---

# Configuring the Google Workspace IdP

Set `IDP_TYPE=google`. The Google provider uses OIDC for login (the `hd` claim
as the domain) and the **Admin SDK Directory API** to resolve group membership,
so it needs both an OAuth client *and* a service account with domain-wide
delegation.

## Prerequisites

- A Google Workspace tenant and a **super-admin** (to grant delegation).
- A Google Cloud project in that org.

## 1. OAuth 2.0 client (login)

In Google Cloud Console → **APIs & Services → Credentials**:

1. Create an **OAuth client ID** of type *Web application*.
2. Add the broker callback as an authorized redirect URI:
   `https://broker.example.com/callback` (this is `IDP_REDIRECT_URL`).
3. Note the **Client ID** and **Client secret** → `IDP_CLIENT_ID` /
   `IDP_CLIENT_SECRET`.

## 2. Service account + domain-wide delegation (groups)

The Directory API call is made as a service account impersonating an admin.

1. In the same project, create a **service account**.
2. Create a **JSON key** for it → mount it and set `GOOGLE_CREDENTIALS_FILE`
   (or rely on Application Default Credentials and leave it empty).
3. Enable the **Admin SDK API** in the project.
4. Note the service account's **OAuth2 client ID** (a numeric "unique ID").

Then in the **Google Admin console** → **Security → API controls → Domain-wide
delegation**, add a new client:

- **Client ID:** the service account's numeric client ID
- **Scopes:** `https://www.googleapis.com/auth/admin.directory.group.readonly`

Finally pick an admin email for the SA to impersonate (must be able to read
groups) → `GOOGLE_DELEGATED_ADMIN`, e.g. `admin@`.

:::caution
Domain-wide delegation is powerful. Grant **only** the read-only group scope
above, and keep the service-account key in a Secret.
:::

## 3. Broker configuration

| Variable | Value |
| --- | --- |
| `IDP_TYPE` | `google` |
| `IDP_ISSUER` | *(optional — defaults to `https://accounts.google.com`)* |
| `IDP_CLIENT_ID` / `IDP_CLIENT_SECRET` | from step 1 |
| `IDP_REDIRECT_URL` | `https://broker.example.com/callback` |
| `IDP_SCOPES` | `openid,email,profile` |
| `GOOGLE_DELEGATED_ADMIN` | admin to impersonate (step 2) |
| `GOOGLE_CREDENTIALS_FILE` | path to the SA JSON key (or empty for ADC) |
| `ALLOWED_DOMAINS` | your Workspace domain(s), matched against `hd` |

`IDP_GROUPS_CLAIM` is **not** used by the Google provider — groups come from the
Directory API, and each `gid` is the Google group id (e.g. `group/g12312312`).

## 4. Helm values

```yaml
broker:
  config:
    idpType: google
    idpRedirectURL: https://broker.example.com/callback
    googleDelegatedAdmin: admin@
    allowedDomains: 
  secret:
    idpClientID: "<oauth-client-id>"
    idpClientSecret: "<oauth-client-secret>"
```

Inject the service-account key file straight from disk with `--set-file`:

```bash
helm install kargus ./chart -n kargus-system \
  --set broker.config.idpType=google \
  --set broker.config.googleDelegatedAdmin=admin@ \
  --set-file broker.secret.googleCredentials=./sa.json
```

When `broker.secret.googleCredentials` is set, the chart stores the key in the
broker Secret, mounts it at `/etc/google/sa.json`, and points
`GOOGLE_CREDENTIALS_FILE` there automatically — no manual volume needed.

:::caution Service-account key required
Domain-wide delegation signs a JWT with `subject = GOOGLE_DELEGATED_ADMIN`, so it
needs the **service-account private key**. Bare Workload Identity / metadata ADC
cannot do this (there's no key to sign with). Provide the key via
`broker.secret.googleCredentials` (or a self-mounted `GOOGLE_CREDENTIALS_FILE`).
The admin must be a **super-admin** of the Workspace, and the SA's client ID must
be authorized for the `admin.directory.group.readonly` scope in that Workspace's
Admin console.
:::

## Troubleshooting

- **`unauthorized_client` on the Directory call** — delegation scope not granted,
  or `GOOGLE_DELEGATED_ADMIN` can't read groups.
- **No groups returned** — the impersonated admin lacks group-read, or the user
  genuinely has no groups (the bind still succeeds with zero memberships).
- **`access_denied` at login** — the user's `hd` is not in `ALLOWED_DOMAINS`, or
  `email_verified` is false.
