{{/* =========================================================================
  Values compatibility with provenance-collector-pack <= 0.1.x
  (docs/src/content/docs/migrating.md).

  Their chart configured the collector CronJob through `config.*`, `schedule`,
  `persistence.mode`, `webUI.*` and `frontend.*`. This chart has none of those
  keys in values.yaml, so anything present under them came from the user:

  - `config.*` keys that have an equivalent are ALIASES: when set they win over
    the `provenance.*` / `scanner.*` value they map to (see
    "provenance-collector.compat.provenance" below).
  - Keys whose behaviour no longer exists FAIL the render with a pointer to
    the replacement (`schedule`, `persistence.mode`, `frontend.keycloak.url`,
    a non-empty `config.namespaces`).
  - Everything else under `config`, `webUI` and `frontend` is accepted and
    ignored; NOTES.txt lists what was ignored.
========================================================================= */}}

{{/* Fail on retired keys. Included once from NOTES.txt and worker.yaml. */}}
{{- define "provenance-collector.compat.validate" -}}
{{- if .Values.schedule }}
{{- fail (printf "`schedule: %q` is from provenance-collector-pack <= 0.1.x (the collector CronJob). Scans now run every scanner.intervalHours hours (default 6); remove `schedule` and set e.g. `--set scanner.intervalHours=24`. See docs: migrating." (toString .Values.schedule)) }}
{{- end }}
{{- if and (kindIs "map" .Values.persistence) (hasKey .Values.persistence "mode") }}
{{- fail (printf "`persistence.mode: %s` is from provenance-collector-pack <= 0.1.x. Reports are now stored in the bundled Postgres (history per scan, last 10 scans) and compliance report files on persistence.reports; there is no http/pvc/configmap mode. Remove persistence.mode (persistence.storageClass and persistence.enabled still apply). See docs: migrating." (toString .Values.persistence.mode)) }}
{{- end }}
{{- $fe := .Values.frontend | default dict }}
{{- if and (kindIs "map" $fe) (kindIs "map" ($fe.keycloak | default dict)) ($fe.keycloak | default dict).url }}
{{- fail "`frontend.keycloak.url` is from provenance-collector-pack <= 0.1.x (in-browser PKCE login). Login is now enforced by the gateway (NebariApp auth) and the API verifies the token and `adminGroups`; remove frontend.* and set nebariapp.hostname / adminGroups. See docs: migrating." }}
{{- end }}
{{- $c := .Values.config | default dict }}
{{- if $c.namespaces }}
{{- fail "`config.namespaces` (scan only these namespaces) is not supported: the scan covers every namespace except scanner.excludedNamespaces. Move the namespaces you do NOT want into scanner.excludedNamespaces (config.excludeNamespaces is accepted as an alias). See docs: migrating." }}
{{- end }}
{{- end }}

{{/* Go duration ("30s", "1m", "1m30s", "500ms") -> whole seconds (min 1). */}}
{{- define "provenance-collector.compat.durationSeconds" -}}
{{- $d := toString . | trim -}}
{{- $total := 0.0 -}}
{{- range $m := regexFindAll "[0-9.]+(ms|h|m|s)" $d -1 -}}
{{- $n := regexFind "^[0-9.]+" $m | float64 -}}
{{- $u := regexReplaceAll "^[0-9.]+" $m "" -}}
{{- if eq $u "h" }}{{ $total = addf $total (mulf $n 3600) }}{{ else if eq $u "m" }}{{ $total = addf $total (mulf $n 60) }}{{ else if eq $u "s" }}{{ $total = addf $total $n }}{{ else if eq $u "ms" }}{{ $total = addf $total (divf $n 1000) }}{{ end -}}
{{- end -}}
{{- if and (eq $total 0.0) (regexMatch "^[0-9]+$" $d) }}{{ $total = float64 $d }}{{ end -}}
{{- max 1 (ceil $total | int) -}}
{{- end }}

{{/*
Effective `provenance` values (JSON) with the legacy `config.*` aliases applied:
  config.verifySignatures -> provenance.verifySignatures
  config.cosignPublicKey  -> provenance.cosignPublicKey (path, KMS URI or PEM)
  config.checkSBOM        -> provenance.checkSBOM
  config.checkProvenance  -> provenance.checkProvenance
  config.checkUpdates     -> provenance.checkUpdates
  config.updateLevel      -> provenance.updateLevel
  config.skipPrerelease   -> provenance.skipPrerelease
  config.helmEnabled      -> provenance.helmReleases.enabled
  config.registryTimeout  -> provenance.registryTimeoutSeconds (Go duration)
Usage: $p := include "provenance-collector.compat.provenance" . | fromJson
*/}}
{{- define "provenance-collector.compat.provenance" -}}
{{- $p := deepCopy .Values.provenance -}}
{{- $c := .Values.config | default dict -}}
{{- range $k := list "verifySignatures" "cosignPublicKey" "checkSBOM" "checkProvenance" "checkUpdates" "updateLevel" "skipPrerelease" -}}
{{- if hasKey $c $k }}{{ $_ := set $p $k (get $c $k) }}{{ end -}}
{{- end -}}
{{- if hasKey $c "helmEnabled" -}}
{{- $_ := set $p.helmReleases "enabled" $c.helmEnabled -}}
{{- end -}}
{{- if hasKey $c "registryTimeout" -}}
{{- $_ := set $p "registryTimeoutSeconds" (include "provenance-collector.compat.durationSeconds" $c.registryTimeout | int) -}}
{{- end -}}
{{- $p | toJson -}}
{{- end }}

{{/* Effective excluded namespaces: scanner.excludedNamespaces + config.excludeNamespaces. */}}
{{- define "provenance-collector.compat.excludedNamespaces" -}}
{{- $c := .Values.config | default dict -}}
{{- concat (.Values.scanner.excludedNamespaces | default list) ($c.excludeNamespaces | default list) | uniq | join "," -}}
{{- end }}

{{/* Effective cluster name: clusterName, else config.clusterName. */}}
{{- define "provenance-collector.compat.clusterName" -}}
{{- $c := .Values.config | default dict -}}
{{- .Values.clusterName | default $c.clusterName | default "" -}}
{{- end }}

{{/* Legacy keys that were accepted and ignored (for NOTES.txt). */}}
{{- define "provenance-collector.compat.ignored" -}}
{{- $out := list -}}
{{- $c := .Values.config | default dict -}}
{{- range $k := list "reportPath" "reportRetention" "reportConfigMap" "reportUploadTimeout" -}}
{{- if hasKey $c $k }}{{ $out = append $out (printf "config.%s" $k) }}{{ end -}}
{{- end -}}
{{- range $top := list "webUI" "frontend" "image" "registryCredentials" "backoffLimit" "activeDeadlineSeconds" "successfulJobsHistoryLimit" "failedJobsHistoryLimit" "concurrencyPolicy" "resources" -}}
{{- if hasKey $.Values $top }}{{ $out = append $out $top }}{{ end -}}
{{- end -}}
{{- join ", " $out -}}
{{- end }}

{{/* Legacy keys that were applied as aliases (for NOTES.txt). */}}
{{- define "provenance-collector.compat.aliased" -}}
{{- $out := list -}}
{{- $c := .Values.config | default dict -}}
{{- range $k := list "verifySignatures" "cosignPublicKey" "checkSBOM" "checkProvenance" "checkUpdates" "updateLevel" "skipPrerelease" "helmEnabled" "registryTimeout" "excludeNamespaces" "clusterName" -}}
{{- if hasKey $c $k }}{{ $out = append $out (printf "config.%s" $k) }}{{ end -}}
{{- end -}}
{{- join ", " $out -}}
{{- end }}
