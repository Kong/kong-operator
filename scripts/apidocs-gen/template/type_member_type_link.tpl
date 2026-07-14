{{- define "type_link" -}}
{{- $typ := . -}}
{{- $typString := $typ | toString -}}

{{- $reg := ".+/.+/.+/api/([a-z-]+)/([a-z0-9]+)\\..+" -}}
{{- $splitDot := splitList "." $typString -}}
{{- $kind := $splitDot | last -}}

{{- if regexMatch $reg $typString }}
{{- $apiGroupVersion := regexReplaceAll $reg $typString "${1}-konghq-com-${2}" -}}
{{- $apiGroupVersion = $apiGroupVersion | trimAll "*[]" -}}
{{- $slicePrefix := "" -}}
{{- if hasPrefix "[]" $typString -}}
{{- $slicePrefix = "[]" -}}
{{- end -}}

{{- if .GVK -}}
{{ $slicePrefix }}[{{ $kind }}](#{{ $apiGroupVersion }}-{{ $kind | lower }})
{{- else -}}
{{ $slicePrefix }}[{{ $kind }}](#{{ $apiGroupVersion }}-types-{{ $kind | lower }})
{{- end -}}

{{- else -}}
{{ $typ }}
{{- end -}}

{{- end -}}
