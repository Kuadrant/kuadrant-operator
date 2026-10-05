{{- define "developer-portal-controller.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
release name wins when it already contains the chart name, so a release named
after the chart yields plain "developer-portal-controller"
*/}}
{{- define "developer-portal-controller.fullname" -}}
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

{{- define "developer-portal-controller.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "developer-portal-controller.labels" -}}
helm.sh/chart: {{ include "developer-portal-controller.chart" . }}
{{ include "developer-portal-controller.selectorLabels" . }}
app.kubernetes.io/name: {{ include "developer-portal-controller.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
deployment selectors are immutable and kuadrant-operator used to create this
deployment inline with exactly these labels; changing them breaks in-place upgrades
*/}}
{{- define "developer-portal-controller.selectorLabels" -}}
app: {{ include "developer-portal-controller.fullname" . }}
control-plane: controller-manager
{{- end }}

{{- define "developer-portal-controller.serviceAccountName" -}}
{{ include "developer-portal-controller.fullname" . }}-manager
{{- end }}

{{/*
digest references are used as-is; an empty tag falls back to v<appVersion>,
or latest for the unreleased 0.0.0
*/}}
{{- define "developer-portal-controller.image" -}}
{{- $repo := .Values.image.repository -}}
{{- if contains "@" $repo -}}
{{ $repo }}
{{- else -}}
{{- $tag := .Values.image.tag | default (ternary "latest" (printf "v%s" .Chart.AppVersion) (eq .Chart.AppVersion "0.0.0")) -}}
{{ $repo }}:{{ $tag }}
{{- end -}}
{{- end }}
