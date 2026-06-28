---
id: helm
title: Helm Chart
sidebar_position: 6
---

# Helm Chart

The chart in `chart/` is an **umbrella chart**: one release installs the
operator, the broker, and the CRD. Components are toggled with
`operator.enabled` / `broker.enabled`.

```bash
helm install kargus ./chart -n kargus-system --create-namespace
```

## Layout

| Path | Purpose |
| --- | --- |
| `chart/crds/` | the `UserAuthenticationBind` CRD (install-once) |
| `chart/templates/operator.yaml` | operator Deployment + ClusterRole + leader-election RBAC |
| `chart/templates/broker.yaml` | broker Deployment, Service, Role, ConfigMap, Secret |
| `chart/templates/redis.yaml` | optional dev Redis (gated by `redis.enabled`) |

## Values

### Operator

| Key | Default | Notes |
| --- | --- | --- |
| `operator.enabled` | `true` | install the controller |
| `operator.replicas` | `1` | |
| `operator.leaderElect` | `true` | adds lease/event RBAC |
| `operator.image.repository` | `ghcr.io/lucasgolino/kargus-operator` | |
| `operator.image.tag` | `latest` | |

### Broker

| Key | Default | Notes |
| --- | --- | --- |
| `broker.enabled` | `true` | install the OIDC broker |
| `broker.replicas` | `1` | scale up only with Redis (see HA) |
| `broker.image.repository` | `git.kargus.io/kargusr` | |
| `broker.service.type` / `broker.service.port` | `ClusterIP` / `80` | |
| `broker.tls.enabled` | `false` | serve HTTPS from the broker (terminate TLS in-process) |
| `broker.tls.secretName` | `""` | `kubernetes.io/tls` Secret (`tls.crt`+`tls.key`); required when enabled. With this on, point the ingress at the backend over HTTPS (e.g. nginx `backend-protocol: HTTPS`) or use TLS passthrough |
| `broker.ingress.enabled` | `false` | expose the broker via an Ingress |
| `broker.ingress.className` | `""` | IngressClass (e.g. `nginx`) |
| `broker.ingress.host` | `""` | defaults to the host of `broker.config.issuer` (keeps the TLS cert SAN in sync). TLS terminates at the ingress; the broker stays HTTP |
| `broker.ingress.annotations` | `{}` | e.g. cert-manager issuer |
| `broker.ingress.tls.enabled` / `broker.ingress.tls.secretName` | `false` / `""` | TLS secret for the host (cert-manager fills it) |
| `broker.ingress.tls.issuer.name` | `""` | cert-manager issuer; when set, adds the issuer annotation |
| `broker.ingress.tls.issuer.kind` | `ClusterIssuer` | `ClusterIssuer` → `cert-manager.io/cluster-issuer`; `Issuer` → `cert-manager.io/issuer` |
| `broker.config.idpType` | `oidc` | `oidc` (generic) \| `google` |
| `broker.config.idpIssuer` | — | IdP issuer URL |
| `broker.config.idpRedirectURL` | — | broker `/callback` registered at the IdP |
| `broker.config.idpScopes` | `openid,email,profile,groups` | request groups for generic OIDC |
| `broker.config.idpGroupsClaim` | `groups` | generic OIDC: claim holding group ids |
| `broker.config.googleDelegatedAdmin` | `""` | required when `idpType=google` |
| `broker.config.*` | — | also: `issuer`, `allowedDomains`, `clientID`, `redirectURIs`, `bindTTL`, `tokenAudiences` |
| `broker.config.bindNamespace` | `""` | defaults to the release namespace |
| `broker.config.sessionStore` | `memory` | `memory` \| `redis` |
| `broker.secret.create` | `true` | render a Secret from the values below |
| `broker.secret.existingSecret` | `""` | use a pre-existing Secret instead |
| `broker.secret.idpClientID` / `idpClientSecret` | `""` | required |
| `broker.secret.idTokenSigningKey` | `""` | RSA PEM to sign id_tokens; **required for >1 replica**. Inject via `--set-file`. Empty = ephemeral key (single-replica dev only) |
| `broker.secret.googleCredentials` | `""` | Google SA JSON key; inject via `--set-file`. Stored in the Secret, mounted at `/etc/google/sa.json`, and `GOOGLE_CREDENTIALS_FILE` set to it automatically |
| `broker.redisAddr` | `""` | external Redis `host:port` |

See the [broker reference](./broker.md) for what each config key means.

### Proxy (`kargus-proxy`)

Separate component; see [Cluster Auth Proxy](./proxy.md).

| Key | Default | Notes |
| --- | --- | --- |
| `proxy.enabled` | `false` | the apiserver auth proxy (no apiserver OIDC needed) |
| `proxy.image.repository` | `ghcr.io/lucasgolino/kargus-proxy` | proxy image (`make build BUNDLE=proxy`) |
| `proxy.image.tag` | `latest` | |
| `proxy.clientID` | `headlamp` | expected `aud` of the broker id_token |
| `proxy.usernameClaim` | `email` | claim mapped to the SA name |
| `proxy.tokenExpirationSeconds` | `600` | minted SA token lifetime |
| `proxy.tls.enabled` | `true` | serve HTTPS |
| `proxy.tls.generate` | `true` | chart self-signs a cert + CA into a Secret (preserved across upgrades); client trusts `ca.crt`. Set false + `secretName` to BYO |
| `proxy.tls.secretName` | `""` | defaults to `<release>-proxy-tls` |
| `proxy.service.port` | `443` | |

### Redis

| Key | Default | Notes |
| --- | --- | --- |
| `redis.enabled` | `false` | dev-only single Redis (no auth/persistence) |
| `redis.image` | `redis:7-alpine` | |

## High availability

The broker keeps auth-requests and one-shot codes in a store. To run more than
one replica, back it with Redis so any replica can complete any leg of the flow:

```bash
# production: bring your own Redis
helm upgrade kargus ./chart -n kargus-system \
  --set broker.replicas=3 \
  --set broker.config.sessionStore=redis \
  --set broker.redisAddr=my-redis:6379

# development: bundled single Redis
helm upgrade kargus ./chart -n kargus-system \
  --set redis.enabled=true \
  --set broker.config.sessionStore=redis
```

When `redis.enabled=true` and `broker.redisAddr` is empty, the chart points the
broker at the bundled Redis automatically.

## Trade-offs

- **CRD in `crds/`** — safe (never deleted on `helm uninstall`, so no accidental
  loss of `UserAuthenticationBind` data), but CRD schema upgrades are manual.
- **Bring-your-own Redis** — no heavyweight Redis subchart; the bundled Redis is
  strictly for development.
- **Umbrella chart** — one version covers both components; to release operator and
  broker independently you would split this into two charts.
- **Bind namespace** — if `broker.config.bindNamespace` differs from the release
  namespace, that namespace must already exist.
