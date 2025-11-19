{{- define "gvDetails" -}}
{{- $gv := . -}}

## <a id="{{ .Group | replace "." "-" }}-{{ .Version }}">{{ $gv.GroupVersionString }}</a>

{{ $gv.Doc }}

{{- if $gv.Kinds }}
{{- range $gv.SortedKinds }}
{{- $typ := $gv.TypeForKind . }}
- [{{ $typ.Name }}](#{{ markdownTypeID $typ | markdownSafeID }})
{{- end }}

{{ end }}

{{- /* Display exported Kinds first */ -}}
{{- range $gv.SortedKinds -}}
{{- $typ := $gv.TypeForKind . }}
{{- $isKind := true -}}
{{ template "type" (dict "type" $typ "isKind" $isKind) }}
{{ end -}}

### Types

In this section you will find types that the CRDs rely on.

{{- /* Display Types that are not exported Kinds */ -}}
{{- range $typ := $gv.SortedTypes -}}
{{- $isKind := false -}}
{{- range $kind := $gv.SortedKinds -}}
{{- if eq $typ.Name $kind -}}
{{- $isKind = true -}}
{{- end -}}
{{- end -}}
{{- if not $isKind }}
{{ template "type" (dict "type" $typ "isKind" $isKind) }}
{{ end -}}
{{- end }}

{{- end -}}
