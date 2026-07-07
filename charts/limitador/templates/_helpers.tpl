{{/*
Expand the name of the chart.
*/}}
{{- define "limitador.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "limitador.fullname" -}}
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

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "limitador.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "limitador.labels" -}}
helm.sh/chart: {{ include "limitador.chart" . }}
{{ include "limitador.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- range $key, $value := .Values.labels }}
{{ $key }}: {{ $value }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "limitador.selectorLabels" -}}
app.kubernetes.io/name: {{ include "limitador.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "limitador.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "limitador.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
