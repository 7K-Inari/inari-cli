{{- define "__HIROBA_NAME__.fullname" -}}
{{ .Release.Name }}
{{- end }}
{{- define "__HIROBA_NAME__.labels" -}}
app.kubernetes.io/name: __HIROBA_NAME__
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
