{{/* Base name */}}
{{- define "kargus.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kargus.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "kargus.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "kargus.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: kargus
{{- end -}}

{{- define "kargus.operator.fullname" -}}{{ include "kargus.fullname" . }}-operator{{- end -}}
{{- define "kargus.broker.fullname" -}}{{ include "kargus.fullname" . }}-broker{{- end -}}
{{- define "kargus.proxy.fullname" -}}{{ include "kargus.fullname" . }}-proxy{{- end -}}
{{- define "kargus.redis.fullname" -}}{{ include "kargus.fullname" . }}-redis{{- end -}}

{{/* Effective proxy TLS secret name */}}
{{- define "kargus.proxy.tlsSecret" -}}
{{- .Values.proxy.tls.secretName | default (printf "%s-tls" (include "kargus.proxy.fullname" .)) -}}
{{- end -}}

{{/* Namespace the broker writes CRs into (defaults to release namespace) */}}
{{- define "kargus.bindNamespace" -}}
{{- default .Release.Namespace .Values.broker.config.bindNamespace -}}
{{- end -}}

{{/* Broker external host: explicit ingress.host, else the host of the issuer URL.
     Keeps the ingress host (and its TLS cert SAN) in sync with ISSUER. */}}
{{- define "kargus.brokerHost" -}}
{{- if .Values.broker.ingress.host -}}
{{- .Values.broker.ingress.host -}}
{{- else -}}
{{- $u := .Values.broker.config.issuer | trimPrefix "https://" | trimPrefix "http://" -}}
{{- (splitList "/" $u) | first -}}
{{- end -}}
{{- end -}}

{{/* Mount path for the injected Google SA key */}}
{{- define "kargus.googleCredsMountPath" -}}/etc/google/sa.json{{- end -}}

{{/* Effective GOOGLE_CREDENTIALS_FILE: the injected key path if provided inline,
     else the operator-managed path from config (self-mounted), else empty */}}
{{- define "kargus.googleCredsFile" -}}
{{- if and .Values.broker.secret.create .Values.broker.secret.googleCredentials -}}
{{- include "kargus.googleCredsMountPath" . -}}
{{- else -}}
{{- .Values.broker.config.googleCredentialsFile -}}
{{- end -}}
{{- end -}}

{{/* Effective Redis address: explicit override, else bundled dev Redis if enabled */}}
{{- define "kargus.redisAddr" -}}
{{- if .Values.broker.redisAddr -}}
{{- .Values.broker.redisAddr -}}
{{- else if .Values.redis.enabled -}}
{{- printf "%s:6379" (include "kargus.redis.fullname" .) -}}
{{- end -}}
{{- end -}}
