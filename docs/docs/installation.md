---
id: installation
title: Installation
sidebar_position: 5
---

# Installation

The recommended install path is the Helm chart in `chart/`, which deploys the
operator, the broker, the (optional) proxy, and the CRD together. The
kubebuilder/kustomize flow is kept below as an alternative for operator-only
setups.

## Prerequisites

- A Kubernetes cluster and a `kubectl` context pointing at it
- Helm 3
- Cluster-admin (the chart installs a CRD and cluster-scoped RBAC)

## Install with Helm

```bash
helm install kargus ./chart \
  --namespace kargus-system --create-namespace \
  --set broker.config.idpType=oidc \
  --set broker.config.idpIssuer=https://idp.example.com \
  --set broker.secret.idpClientID=<id> \
  --set broker.secret.idpClientSecret=<secret> \
  --set broker.config.issuer=https://broker.example.com \
  --set broker.config.idpRedirectURL=https://broker.example.com/callback \
  --set broker.config.allowedDomains= \
  --set broker.config.redirectURIs=https://client.example.com/oidc/callback
```

See the [Helm chart reference](./helm.md) for all values, HA/Redis options, and
trade-offs.

## Cluster access (proxy + client)

The steps above provision binds. For a user's login to actually reach the
kube-apiserver, enable the proxy and point your OIDC client at it:

```bash
helm upgrade kargus ./chart -n kargus-system --reuse-values \
  --set proxy.enabled=true --set proxy.clientID=<client-id>
```

The proxy self-signs its TLS by default. Then configure the client to send the
broker id_token to the proxy and trust its CA — see the
[Cluster auth proxy](./proxy.md) page and the Headlamp example in
`assets/headlamp-values.yaml`.

## Try it

1. Annotate a role you want to expose:

   ```bash
   kubectl annotate clusterrole view rbac.kargus.io/group=group/g12312312
   ```

2. Apply a bind (the operator ships a sample):

   ```bash
   kubectl apply -f operator/config/samples/v1_userauthenticationbind.yaml
   ```

3. Watch it converge:

   ```bash
   kubectl get userauthenticationbind -n kargus-system
   # NAME   USER    DOMAIN            PHASE    EXPIRES   AGE
   # ...    lucas      binded   12h       5s
   ```

4. Confirm the binding exists:

   ```bash
   kubectl get clusterrolebindings -l kargus.io/owned-by
   ```

## Uninstall

```bash
helm uninstall kargus -n kargus-system
```

Deleting a `UserAuthenticationBind` triggers its finalizer, which removes every
owned `ClusterRoleBinding`/`RoleBinding`; the `ServiceAccount` is garbage
collected via its owner reference.

:::note CRD lifecycle
The CRD ships in `chart/crds/`, so Helm installs it once but never upgrades or
deletes it. To update the CRD schema, apply it manually; `helm uninstall` leaves
the CRD (and your `UserAuthenticationBind` data) in place.
:::

## Alternative: operator-only via kustomize

```bash
cd operator
make install                       # CRD only
export IMG=<registry>/kargus-operator:tag
make docker-build docker-push IMG=$IMG
make deploy IMG=$IMG               # operator into operator-system
```
