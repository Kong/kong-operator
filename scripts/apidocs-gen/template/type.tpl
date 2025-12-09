{{- define "type" -}}
{{- $type := $.type -}}
{{- $isKind := $.isKind -}}
{{- if markdownShouldRenderType $type -}}

{{- if $isKind -}}
### {{ $type.Name }}
{{ else -}}
#### {{ $type.Name }}
{{ end -}}

{{- if $type.IsAlias }}
_Underlying type:_ `{{ markdownRenderTypeLink $type.UnderlyingType }}`
{{- end }}

{{ $type.Doc | replace "\n\n" "<br /><br />" }}

{{ if $type.GVK -}}
<!-- {{ snakecase $type.Name }} description placeholder -->
{{- end }}

{{ if $type.Members -}}
| Field | Description |
| --- | --- |
{{ if $type.GVK -}}
| `apiVersion` _string_ | `{{ $type.GVK.Group }}/{{ $type.GVK.Version }}`
| `kind` _string_ | `{{ $type.GVK.Kind }}`
{{ end -}}

{{- $regK8s := "k8s\\.io/api/.*" -}}

{{ range $type.Members -}}
{{- $typString := .Type | toString -}}
{{ if regexMatch $regK8s $typString -}}
| `{{ .Name }}` _{{ markdownRenderType .Type }}_ | {{ template "type_members" . }} |
{{ else -}}
| `{{ .Name }}` _{{- template "type_link" .Type -}}_ | {{ template "type_members" . }} |
{{ end -}}
{{ end -}}
{{ end -}}


{{- if $type.References }}
_Appears in:_
{{ range $type.SortedReferences }}
- {{ template "type_link" . }}
{{- end }}
{{- end }}

{{- if $type.EnumValues }}

Allowed values:

| Value | Description |
| --- | --- |
{{- range $type.EnumValues }}
| `{{ .Name }}` | {{ markdownRenderFieldDoc .Doc }} |
{{- end }}
{{- end }}

{{- end -}}
{{- end -}}
