{{- define "type_link" -}}
{{- $typ := . -}}
{{- $typString := $typ | toString -}}

{{- $reg := ".+/.+/.+/api/([a-z-]+)/([a-z0-9]+)\\..+" -}}
{{- $splitDot := splitList "." $typString -}}
{{- $kind := $splitDot | last -}}

{{- if regexMatch $reg $typString }}
{{- $apiGroupVersion := regexReplaceAll $reg $typString "${1}-konghq-com-${2}" -}}
{{- $apiGroupVersion = $apiGroupVersion | trimAll "*[]" -}}
[{{ $kind }}](#{{ $apiGroupVersion }}-types-{{ $kind | lower }})
{{- else -}}
{{ $typ }}
{{- end -}}

{{- end -}}
