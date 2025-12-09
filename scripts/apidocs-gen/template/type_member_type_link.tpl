{{- define "type_link" -}}
{{- $typ := . -}}
{{- $typString := $typ | toString -}}

{{- $reg := ".+/.+/.+/api/([a-z-]+)/([a-z0-9]+)\\..+" -}}
{{- $splitDot := splitList "." $typString -}}
{{- $kind := $splitDot | last -}}

{{- if regexMatch $reg $typString }}
{{- $apiGroupVersion := regexReplaceAll $reg $typString "${1}-konghq-com-${2}" -}}
{{- $apiGroupVersion = $apiGroupVersion | trimAll "*[]" -}}

{{- if .GVK -}}
[{{ $kind }}](#{{ $apiGroupVersion }}-{{ $kind | lower }})
{{- else -}}
[{{ $kind }}](#{{ $apiGroupVersion }}-types-{{ $kind | lower }})
{{- end -}}

{{- else -}}
{{ $typ }}
{{- end -}}

{{- end -}}
