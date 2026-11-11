{{/* Names */}}
{{- define "airval.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "airval.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "airval.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "airval.labels" -}}
helm.sh/chart: {{ include "airval.chart" . }}
{{ include "airval.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: airval
{{- end }}

{{- define "airval.selectorLabels" -}}
app.kubernetes.io/name: {{ include "airval.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "airval.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "airval.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "airval.image" -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) }}
{{- end }}

{{/* Name of the Secret created by this chart (also holds the generated master key). */}}
{{- define "airval.secretName" -}}
{{- include "airval.fullname" . }}
{{- end }}

{{/* Secret holding the master key. */}}
{{- define "airval.masterKeySecret" -}}
{{- default (include "airval.secretName" .) .Values.secrets.existingSecret }}
{{- end }}

{{/* Does the chart need to create a Secret? */}}
{{- define "airval.createSecret" -}}
{{- if or (not .Values.secrets.existingSecret) .Values.postgresql.dev.enabled (and (not .Values.postgresql.cnpg.enabled) (not .Values.postgresql.external.existingSecret) .Values.postgresql.external.url) (and (not .Values.secrets.existingSecret) .Values.bootstrap.adminPassword) -}}
true
{{- end }}
{{- end }}

{{/* Fail early on inconsistent values. */}}
{{- define "airval.validate" -}}
{{- if and .Values.worker.hpa.enabled .Values.worker.keda.enabled }}
{{- fail "worker.hpa.enabled and worker.keda.enabled are mutually exclusive" }}
{{- end }}
{{- if and .Values.ingress.enabled .Values.gateway.enabled }}
{{- fail "ingress.enabled and gateway.enabled are mutually exclusive: pick one" }}
{{- end }}
{{- if and .Values.nats.enabled .Values.nats.external.url }}
{{- fail "nats.enabled=true and nats.external.url are mutually exclusive: set nats.enabled=false to use an external NATS" }}
{{- end }}
{{- if and (not .Values.nats.enabled) (not .Values.nats.external.url) }}
{{- fail "set nats.external.url (nats://host:4222) or enable the bundled nats subchart (nats.enabled=true)" }}
{{- end }}
{{- if and .Values.postgresql.cnpg.enabled .Values.postgresql.dev.enabled }}
{{- fail "postgresql.cnpg.enabled and postgresql.dev.enabled are mutually exclusive" }}
{{- end }}
{{- if and (or .Values.postgresql.cnpg.enabled .Values.postgresql.dev.enabled) (or .Values.postgresql.external.url .Values.postgresql.external.existingSecret) }}
{{- fail "postgresql.cnpg.enabled / postgresql.dev.enabled and postgresql.external.* are mutually exclusive" }}
{{- end }}
{{- if not (or .Values.postgresql.cnpg.enabled .Values.postgresql.dev.enabled .Values.postgresql.external.url .Values.postgresql.external.existingSecret) }}
{{- fail "set postgresql.external.url (or postgresql.external.existingSecret), or enable postgresql.cnpg.enabled / postgresql.dev.enabled for an evaluation database" }}
{{- end }}
{{- end }}

{{/* Fullname of the bundled NATS subchart's Service (mirrors the subchart's nats.fullname helper). */}}
{{- define "airval.natsFullname" -}}
{{- if .Values.nats.fullnameOverride }}
{{- .Values.nats.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else if contains "nats" .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-nats" .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{- define "airval.natsURL" -}}
{{- if .Values.nats.enabled }}
{{- printf "nats://%s:4222" (include "airval.natsFullname" .) }}
{{- else }}
{{- .Values.nats.external.url }}
{{- end }}
{{- end }}

{{/* Name of the CloudNativePG cluster. */}}
{{- define "airval.cnpgName" -}}
{{- printf "%s-pg" (include "airval.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Name of the dev-only bundled Postgres. */}}
{{- define "airval.devPgName" -}}
{{- printf "%s-postgres" (include "airval.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* True when the database is created by this release, so it cannot exist yet for the pre-install migration Job. */}}
{{- define "airval.bundledDB" -}}
{{- if or .Values.postgresql.cnpg.enabled .Values.postgresql.dev.enabled -}}true{{- end }}
{{- end }}

{{/* AIRVAL_DATABASE_URL env entry. */}}
{{- define "airval.databaseURLEnv" -}}
- name: AIRVAL_DATABASE_URL
  valueFrom:
    secretKeyRef:
      {{- if .Values.postgresql.cnpg.enabled }}
      name: {{ include "airval.cnpgName" . }}-app
      key: uri
      {{- else if .Values.postgresql.external.existingSecret }}
      name: {{ .Values.postgresql.external.existingSecret }}
      key: {{ .Values.postgresql.external.existingSecretKey }}
      {{- else }}
      name: {{ include "airval.secretName" . }}
      key: database-url
      {{- end }}
{{- end }}

{{/*
Environment shared by server, worker and the migration Job.
Call with a dict: (dict "ctx" $ "role" "server|worker|migrate")
*/}}
{{- define "airval.env" -}}
{{- $ := .ctx }}
{{- $role := .role }}
{{ include "airval.databaseURLEnv" $ }}
- name: AIRVAL_NATS_URL
  value: {{ include "airval.natsURL" $ | quote }}
{{- if $.Values.nats.enabled }}
- name: AIRVAL_NATS_REPLICAS
  value: {{ $.Values.nats.config.cluster.replicas | quote }}
{{- end }}
- name: AIRVAL_SECRETS_MASTER_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "airval.masterKeySecret" $ }}
      key: {{ $.Values.secrets.keys.masterKey }}
- name: AIRVAL_SECRETS_KEY_VERSION
  value: {{ $.Values.secrets.keyVersion | quote }}
- name: AIRVAL_SECRETS_PREVIOUS_KEYS
  valueFrom:
    secretKeyRef:
      name: {{ include "airval.masterKeySecret" $ }}
      key: {{ $.Values.secrets.keys.previousKeys }}
      optional: true
{{- if $.Values.postgresql.external.maxOpenConns }}
- name: AIRVAL_DATABASE_MAX_OPEN_CONNS
  value: {{ $.Values.postgresql.external.maxOpenConns | quote }}
{{- end }}
{{- if ne $role "migrate" }}
- name: AIRVAL_DATABASE_AUTO_MIGRATE
  # The migration Job owns schema changes; with an evaluation database (CNPG or dev) the server migrates itself on first start.
  value: {{ if and (eq $role "server") (include "airval.bundledDB" $) }}"true"{{ else }}"false"{{ end }}
- name: AIRVAL_SERVER_ADDR
  value: ":8080"
- name: AIRVAL_SERVER_BASE_URL
  value: {{ default "http://localhost:8080" $.Values.config.baseUrl | quote }}
- name: AIRVAL_AUTH_COOKIE_SECURE
  value: {{ $.Values.config.cookieSecure | quote }}
- name: AIRVAL_AUTH_ALLOW_SIGNUP
  value: {{ $.Values.config.allowSignup | quote }}
- name: AIRVAL_TELEMETRY_LOG_LEVEL
  value: {{ $.Values.config.logLevel | quote }}
- name: AIRVAL_TELEMETRY_LOG_FORMAT
  value: {{ $.Values.config.logFormat | quote }}
- name: AIRVAL_TELEMETRY_SAMPLE_RATIO
  value: {{ $.Values.config.traceSampleRatio | quote }}
{{- if $.Values.config.otlpEndpoint }}
- name: AIRVAL_TELEMETRY_OTLP_ENDPOINT
  value: {{ $.Values.config.otlpEndpoint | quote }}
{{- end }}
{{- end }}
{{- if eq $role "server" }}
{{- if $.Values.bootstrap.adminEmail }}
- name: AIRVAL_BOOTSTRAP_ADMIN_EMAIL
  value: {{ $.Values.bootstrap.adminEmail | quote }}
- name: AIRVAL_BOOTSTRAP_ADMIN_NAME
  value: {{ $.Values.bootstrap.adminName | quote }}
- name: AIRVAL_BOOTSTRAP_ORG_NAME
  value: {{ $.Values.bootstrap.orgName | quote }}
- name: AIRVAL_BOOTSTRAP_ADMIN_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "airval.masterKeySecret" $ }}
      key: {{ $.Values.secrets.keys.bootstrapAdminPassword }}
      optional: true
{{- end }}
{{- end }}
{{- if eq $role "worker" }}
- name: AIRVAL_WORKERS_CONCURRENCY
  value: {{ $.Values.worker.concurrency | quote }}
- name: AIRVAL_WORKERS_JUDGE_CONCURRENCY
  value: {{ $.Values.worker.judgeConcurrency | quote }}
{{- end }}
{{- range $k, $v := $.Values.config.env }}
- name: {{ $k }}
  value: {{ $v | quote }}
{{- end }}
{{- with $.Values.config.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/* Pod scheduling knobs. Call with (dict "ctx" $ "v" .Values.server). */}}
{{- define "airval.scheduling" -}}
{{- with .v.nodeSelector }}
nodeSelector: {{ toYaml . | nindent 2 }}
{{- end }}
{{- with .v.affinity }}
affinity: {{ toYaml . | nindent 2 }}
{{- end }}
{{- with .v.tolerations }}
tolerations: {{ toYaml . | nindent 2 }}
{{- end }}
{{- with .v.topologySpreadConstraints }}
topologySpreadConstraints: {{ toYaml . | nindent 2 }}
{{- end }}
{{- with .v.priorityClassName }}
priorityClassName: {{ . }}
{{- end }}
{{- end }}
