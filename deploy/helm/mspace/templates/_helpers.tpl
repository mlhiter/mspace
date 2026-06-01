{{- define "mspace.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "mspace.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "mspace.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "mspace.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: mspace
{{- end -}}

{{- define "mspace.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mspace.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "mspace.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "mspace.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "mspace.secretName" -}}
{{- if .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret -}}
{{- else -}}
{{- printf "%s-secrets" (include "mspace.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "mspace.runtimeTokenSecretName" -}}
{{- if .Values.secrets.runtimeTokenExistingSecret -}}
{{- .Values.secrets.runtimeTokenExistingSecret -}}
{{- else -}}
{{- include "mspace.secretName" . -}}
{{- end -}}
{{- end -}}

{{- define "mspace.runtimeTokenSecretKey" -}}
{{- if .Values.secrets.runtimeTokenExistingSecret -}}
{{- default "MSPACE_RUNTIME_TOKEN" .Values.secrets.runtimeTokenExistingSecretKey -}}
{{- else -}}
MSPACE_RUNTIME_TOKEN
{{- end -}}
{{- end -}}

{{- define "mspace.runtimeTokenValue" -}}
{{- if .Values.secrets.runtimeToken -}}
{{- .Values.secrets.runtimeToken -}}
{{- else -}}
{{- $existingSecret := lookup "v1" "Secret" .Release.Namespace (include "mspace.secretName" .) -}}
{{- if and $existingSecret $existingSecret.data (index $existingSecret.data "MSPACE_RUNTIME_TOKEN") -}}
{{- index $existingSecret.data "MSPACE_RUNTIME_TOKEN" | b64dec -}}
{{- else -}}
{{- printf "msw_%s" (randAlphaNum 64 | lower) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "mspace.codexHomeSecretName" -}}
{{- if .Values.codexHome.existingSecret -}}
{{- .Values.codexHome.existingSecret -}}
{{- end -}}
{{- end -}}

{{- define "mspace.codexHomeAuthKey" -}}
{{- if .Values.codexHome.existingSecret -}}
{{- default "auth.json" .Values.codexHome.authKey -}}
{{- end -}}
{{- end -}}

{{- define "mspace.codexHomeConfigKey" -}}
{{- if .Values.codexHome.existingSecret -}}
{{- default "config.toml" .Values.codexHome.configKey -}}
{{- end -}}
{{- end -}}

{{- define "mspace.postgresName" -}}
{{- printf "%s-postgres" (include "mspace.fullname" .) -}}
{{- end -}}

{{- define "mspace.databaseUrl" -}}
{{- if .Values.secrets.databaseUrl -}}
{{- .Values.secrets.databaseUrl -}}
{{- else if .Values.postgresql.enabled -}}
{{- printf "postgres://%s:%s@%s:5432/%s?sslmode=disable" .Values.postgresql.auth.username .Values.postgresql.auth.password (include "mspace.postgresName" .) .Values.postgresql.auth.database -}}
{{- else -}}
{{- "" -}}
{{- end -}}
{{- end -}}

{{- define "mspace.serverImage" -}}
{{- printf "%s:%s" .Values.server.image.repository .Values.server.image.tag -}}
{{- end -}}

{{- define "mspace.workerImage" -}}
{{- printf "%s:%s" .Values.worker.image.repository .Values.worker.image.tag -}}
{{- end -}}

{{- define "mspace.workerCapabilities" -}}
{{- if .Values.worker.capabilities -}}
{{- .Values.worker.capabilities -}}
{{- else -}}
{{- dict "protocolSmoke" true "codex" true "docker" true "kubectl" true "buildkit" .Values.buildkit.enabled "dryRun" false | toJson -}}
{{- end -}}
{{- end -}}

{{- define "mspace.validateWorkerCodexHome" -}}
{{- if .Values.worker.enabled -}}
{{- if not .Values.codexHome.existingSecret -}}
{{- fail "worker.enabled=true requires codexHome.existingSecret with worker Codex auth/config" -}}
{{- end -}}
{{- if not .Values.codexHome.authKey -}}
{{- fail "worker.enabled=true requires codexHome.authKey, usually auth.json" -}}
{{- end -}}
{{- if not .Values.codexHome.configKey -}}
{{- fail "worker.enabled=true requires codexHome.configKey, usually config.toml" -}}
{{- end -}}
{{- if and (not .Values.bootstrap.teamWorkspace.enabled) (not .Values.secrets.runtimeTokenExistingSecret) (not .Values.secrets.runtimeToken) -}}
{{- fail "worker.enabled=true requires bootstrap.teamWorkspace.enabled=true or an explicit runtime token Secret/value" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "mspace.validateRuntimeTokenValue" -}}
{{- if and .Values.secrets.runtimeToken (not (hasPrefix "msw_" .Values.secrets.runtimeToken)) -}}
{{- fail "secrets.runtimeToken must start with msw_" -}}
{{- end -}}
{{- if and .Values.secrets.runtimeToken (lt (len (trimPrefix "msw_" .Values.secrets.runtimeToken)) 32) -}}
{{- fail "secrets.runtimeToken must contain at least 32 characters after msw_" -}}
{{- end -}}
{{- end -}}

{{- define "mspace.validateBootstrapTeamWorkspace" -}}
{{- if .Values.bootstrap.teamWorkspace.enabled -}}
{{- if and (not .Values.secrets.existingSecret) (not .Values.secrets.bootstrapAdminLogin) -}}
{{- fail "bootstrap.teamWorkspace.enabled=true requires secrets.bootstrapAdminLogin or secrets.existingSecret" -}}
{{- end -}}
{{- if and (not .Values.secrets.existingSecret) (not .Values.secrets.bootstrapAdminPassword) -}}
{{- fail "bootstrap.teamWorkspace.enabled=true requires secrets.bootstrapAdminPassword or secrets.existingSecret" -}}
{{- end -}}
{{- if or (lt (int .Values.bootstrap.runtimeToken.expiresInHours) 1) (gt (int .Values.bootstrap.runtimeToken.expiresInHours) 2160) -}}
{{- fail "bootstrap.runtimeToken.expiresInHours must be between 1 and 2160" -}}
{{- end -}}
{{- end -}}
{{- end -}}
