{{/*
Selector-Labels: eindeutig je Anwendung, damit jeder Service nur die Pods seines eigenen Deployments selektiert.
*/}}
{{- define "demo-app.selectorLabels" -}}
app.kubernetes.io/name: {{ .Values.name }}
{{- end }}

{{/*
Alle Labels: die konfigurierten .Values.labels (z. B. app.kubernetes.io/part-of) plus die Selector-Labels.
*/}}
{{- define "demo-app.labels" -}}
{{- with .Values.labels }}{{ toYaml . }}
{{ end -}}
{{ include "demo-app.selectorLabels" . }}
{{- end }}
