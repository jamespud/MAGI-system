{{- define "magi.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "magi.fullname" -}}
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

{{- define "magi.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "magi.labels" -}}
helm.sh/chart: {{ include "magi.chart" . }}
{{ include "magi.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "magi.selectorLabels" -}}
app.kubernetes.io/name: {{ include "magi.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "magi.serviceAccountName" -}}
{{- if .Values.common.serviceAccount.create -}}
{{- default (include "magi.fullname" .) .Values.common.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.common.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "magi.secretName" -}}
{{- if .Values.secret.existingSecret -}}
{{- .Values.secret.existingSecret -}}
{{- else -}}
{{- include "magi.fullname" . -}}
{{- end -}}
{{- end -}}

{{- define "magi.backendImage" -}}
{{- $tag := default .Chart.AppVersion .Values.backend.image.tag -}}
{{- printf "%s:%s" .Values.backend.image.repository $tag -}}
{{- end -}}

{{- define "magi.frontendImage" -}}
{{- $tag := default .Chart.AppVersion .Values.frontend.image.tag -}}
{{- printf "%s:%s" .Values.frontend.image.repository $tag -}}
{{- end -}}
