{{- define "type_members" -}}
{{- $field := . -}}
{{- if eq $field.Name "metadata" -}}
Refer to Kubernetes API documentation for fields of `metadata`.
{{- else -}}

{{- /* First replace makes paragraphs separated, second merges lines in paragraphs. */ -}}
{{- $doc := $field.Doc | replace "\n\n" "<br /><br />" |  replace "\n" " " -}}
{{- $doc = $doc | replace "  -" "<br />  -" -}}
{{ $doc }}
{{- end -}}
{{- end -}}
