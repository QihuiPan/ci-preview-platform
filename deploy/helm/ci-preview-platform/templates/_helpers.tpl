{{- define "ci-preview-platform.name" -}}
ci-preview-platform
{{- end -}}

{{- define "ci-preview-platform.labels" -}}
app.kubernetes.io/name: {{ include "ci-preview-platform.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}
