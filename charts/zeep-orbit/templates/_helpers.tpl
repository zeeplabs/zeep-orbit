{{- define "zeep-orbit.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "zeep-orbit.fullname" -}}
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

{{- define "zeep-orbit.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "zeep-orbit.labels" -}}
helm.sh/chart: {{ include "zeep-orbit.chart" . }}
{{ include "zeep-orbit.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}

{{- define "zeep-orbit.selectorLabels" -}}
app.kubernetes.io/name: {{ include "zeep-orbit.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "zeep-orbit.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "zeep-orbit.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "zeep-orbit.image" -}}
{{- $repo := .Values.image.repository }}
{{- $tag := .Values.image.tag | default .Chart.AppVersion }}
{{- if .Values.image.digest }}
{{- printf "%s@%s" $repo .Values.image.digest }}
{{- else }}
{{- printf "%s:%s" $repo $tag }}
{{- end }}
{{- end }}

{{- define "zeep-orbit.getKubeVersion" -}}
{{- default "1.27" (printf "%s.%s" (toString .Capabilities.KubeVersion.Major) (toString .Capabilities.KubeVersion.Minor)) }}
{{- end }}

{{- define "zeep-orbit.builder.fullname" -}}
{{- printf "%s-builder" (include "zeep-orbit.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "zeep-orbit.registryProxy.fullname" -}}
{{- printf "%s-registry-proxy" (include "zeep-orbit.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "zeep-orbit.builder.secretName" -}}
{{- default (include "zeep-orbit.fullname" .) .Values.builder.existingSecret }}
{{- end }}

{{- define "zeep-orbit.builder.image" -}}
{{- printf "%s:%s" .Values.builder.image.repository (default .Chart.AppVersion .Values.builder.image.tag) }}
{{- end }}

{{- define "zeep-orbit.registryProxy.image" -}}
{{- printf "%s:%s" .Values.builder.registryProxy.image.repository (default .Chart.AppVersion .Values.builder.registryProxy.image.tag) }}
{{- end }}

{{/*
The builder and proxy pods use their own app.kubernetes.io/name so the Orbit
Service and Deployment selectors (name + instance) never match them.
*/}}
{{- define "zeep-orbit.builder.selectorLabels" -}}
app.kubernetes.io/name: {{ printf "%s-builder" (include "zeep-orbit.name" .) | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: builder
{{- end }}

{{- define "zeep-orbit.registryProxy.selectorLabels" -}}
app.kubernetes.io/name: {{ printf "%s-registry-proxy" (include "zeep-orbit.name" .) | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: registry-proxy
{{- end }}
