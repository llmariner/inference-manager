{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "inference-manager-server.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "inference-manager-server.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "inference-manager-server.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "inference-manager-server.labels" -}}
helm.sh/chart: {{ include "inference-manager-server.chart" . }}
{{ include "inference-manager-server.selectorLabels" . }}
{{- if .Values.version }}
app.kubernetes.io/version: {{ .Values.version | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels
*/}}
{{- define "inference-manager-server.selectorLabels" -}}
app.kubernetes.io/name: {{ include "inference-manager-server.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Create the name of the service account to use
*/}}
{{- define "inference-manager-server.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
    {{ default (include "inference-manager-server.fullname" .) .Values.serviceAccount.name }}
{{- else -}}
    {{ default "default" .Values.serviceAccount.name }}
{{- end -}}
{{- end -}}

{{/*
metrics port
*/}}
{{- define "inference-manager-server.metricsPort" -}}
{{- if .Values.kubernetesManager.metricsBindAddress -}}
{{ mustRegexSplit ":" .Values.kubernetesManager.metricsBindAddress -1 | last }}
{{- else -}}
8080
{{- end -}}
{{- end -}}

{{/*
health port
*/}}
{{- define "inference-manager-server.healthPort" -}}
{{- if .Values.kubernetesManager.healthBindAddress -}}
{{ mustRegexSplit ":" .Values.kubernetesManager.healthBindAddress -1 | last }}
{{- end -}}
{{- end -}}

{{/*
pprof port
*/}}
{{- define "inference-manager-server.pprofPort" -}}
{{- if .Values.kubernetesManager.pprofBindAddress -}}
{{ mustRegexSplit ":" .Values.kubernetesManager.pprofBindAddress -1 | last }}
{{- end -}}
{{- end -}}
