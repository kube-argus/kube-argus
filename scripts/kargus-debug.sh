#!/usr/bin/env bash
# kargus-debug.sh — collect everything needed to diagnose the broker OIDC flow,
# focused on the "code not found" / missing-/callback class of bugs.
#
# Usage:
#   scripts/kargus-debug.sh [-n NAMESPACE]
#
# Read-only: it only gets/describes/logs and curls the public discovery
# endpoints. Requires: kubectl (cluster access), curl, jq (optional).

set -uo pipefail

NS="${NS:-kargus-system}"
while getopts "n:" opt; do
  case "$opt" in
    n) NS="$OPTARG" ;;
    *) echo "usage: $0 [-n namespace]"; exit 1 ;;
  esac
done

BROKER_SEL="app.kubernetes.io/component=broker"
hr() { printf '\n=== %s ===\n' "$1"; }
have() { command -v "$1" >/dev/null 2>&1; }

# --- locate the broker deployment + its config/secret -------------------------
DEPLOY="$(kubectl -n "$NS" get deploy -l "$BROKER_SEL" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)"
if [ -z "${DEPLOY:-}" ]; then
  echo "No broker deployment (label $BROKER_SEL) in namespace $NS. Wrong -n?" >&2
  exit 1
fi
CM="$(kubectl -n "$NS" get deploy "$DEPLOY" -o jsonpath='{.spec.template.spec.containers[0].envFrom[*].configMapRef.name}' 2>/dev/null)"
SECRET="$(kubectl -n "$NS" get deploy "$DEPLOY" -o jsonpath='{.spec.template.spec.containers[0].envFrom[*].secretRef.name}' 2>/dev/null)"

hr "Deployment"
echo "namespace : $NS"
echo "deployment: $DEPLOY"
echo "configmap : ${CM:-<none>}"
echo "secret    : ${SECRET:-<none>}"
kubectl -n "$NS" get deploy "$DEPLOY" \
  -o jsonpath='replicas={.spec.replicas}{"\n"}image={.spec.template.spec.containers[0].image}{"\n"}' 2>/dev/null

hr "Pods"
kubectl -n "$NS" get pods -l "$BROKER_SEL" -o wide 2>/dev/null

# --- non-secret config --------------------------------------------------------
get() { kubectl -n "$NS" get cm "$CM" -o jsonpath="{.data.$1}" 2>/dev/null; }
ISSUER="$(get ISSUER)"
IDP_REDIRECT_URL="$(get IDP_REDIRECT_URL)"
REDIRECT_URIS="$(get REDIRECT_URIS)"

hr "Broker config (ConfigMap $CM)"
for k in ISSUER IDP_TYPE IDP_ISSUER IDP_REDIRECT_URL IDP_SCOPES IDP_GROUPS_CLAIM \
         GOOGLE_DELEGATED_ADMIN ALLOWED_DOMAINS CLIENT_ID REDIRECT_URIS \
         BIND_NAMESPACE BIND_TTL TOKEN_AUDIENCES TOKEN_LIFETIME_SECONDS \
         REFRESH_TOKEN_LIFETIME_SECONDS SESSION_STORE REDIS_ADDR; do
  printf '%-32s %s\n' "$k" "$(get "$k")"
done

hr "Secret keys present (values hidden) — $SECRET"
kubectl -n "$NS" get secret "$SECRET" -o go-template='{{range $k,$v := .data}}{{$k}}{{"\n"}}{{end}}' 2>/dev/null

# --- the redirect-URI sanity check (the prime suspect) ------------------------
hr "Redirect-URI check"
iss_host="$(printf '%s' "$ISSUER"            | sed -E 's#https?://([^/]+).*#\1#')"
idp_host="$(printf '%s' "$IDP_REDIRECT_URL"  | sed -E 's#https?://([^/]+).*#\1#')"
idp_path="$(printf '%s' "$IDP_REDIRECT_URL"  | sed -E 's#https?://[^/]+##')"
echo "ISSUER                = $ISSUER"
echo "IDP_REDIRECT_URL      = $IDP_REDIRECT_URL   (broker -> IdP callback)"
echo "REDIRECT_URIS(client) = $REDIRECT_URIS      (the client's callback)"
echo
if [ "$idp_host" != "$iss_host" ] || ! printf '%s' "$idp_path" | grep -q '/callback$'; then
  echo "  ⚠️  IDP_REDIRECT_URL should be the BROKER callback, i.e. https://$iss_host/callback"
  echo "      It currently points at host '$idp_host' path '$idp_path'."
  echo "      If this is the client's URL, the IdP redirects past the broker /callback"
  echo "      and /token gets a code the broker never issued -> 'code not found'."
else
  echo "  ✓ IDP_REDIRECT_URL host matches the issuer and ends in /callback."
fi
if [ "$IDP_REDIRECT_URL" = "$REDIRECT_URIS" ]; then
  echo "  ⚠️  IDP_REDIRECT_URL == REDIRECT_URIS — these must differ (broker vs client callback)."
fi

# --- discovery + jwks (must reflect the signed-id_token build) ----------------
hr "Discovery + JWKS (public, via $ISSUER)"
if [ -n "$ISSUER" ] && have curl; then
  disc="$(curl -fsS "$ISSUER/.well-known/openid-configuration" 2>/dev/null)"
  if [ -n "$disc" ]; then
    if have jq; then printf '%s' "$disc" | jq '{issuer,token_endpoint,jwks_uri,grant_types_supported,id_token_signing_alg_values_supported}'
    else printf '%s\n' "$disc"; fi
  else
    echo "  could not fetch $ISSUER/.well-known/openid-configuration (DNS/TLS/ingress from here?)"
  fi
  kid="$(curl -fsS "$ISSUER/jwks" 2>/dev/null | { have jq && jq -r '.keys[0].kid' || cat; })"
  echo "jwks kid: ${kid:-<none — old image or /jwks unreachable>}"
else
  echo "  skipped (no ISSUER or no curl)"
fi

# --- broker logs: is /callback ever hit? what does /token say? ----------------
hr "Broker logs (last 200 lines, filtered)"
kubectl -n "$NS" logs -l "$BROKER_SEL" --prefix --tail=200 2>/dev/null \
  | grep -E '/callback|/token|code not found|client/redirect|PKCE|idp authentication|refresh token' \
  || echo "  (no matching log lines)"

echo
CB="$(kubectl -n "$NS" logs -l "$BROKER_SEL" --tail=500 2>/dev/null | grep -c '/callback')"
echo "  /callback occurrences in last 500 lines: $CB"
[ "$CB" = "0" ] && echo "  ⚠️  /callback never hit -> the IdP is not redirecting to the broker (see Redirect-URI check)."

# --- operator side: did any binds get created / their phase? ------------------
hr "UserAuthenticationBind CRs"
kubectl -n "$NS" get userauthenticationbind \
  -o custom-columns='NAME:.metadata.name,USER:.spec.user,PHASE:.status.sv.status,REF:.status.sv.ref' 2>/dev/null \
  || echo "  (none / CRD not installed)"

hr "Done"
echo "Most common cause of 'code not found' with no /callback: IDP_REDIRECT_URL pointing"
echo "at the client instead of the broker. See the Redirect-URI check above."
