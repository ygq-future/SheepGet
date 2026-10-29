---
name: sheep-get
description: Trigger and manage file downloads, persistent artifacts, media, or HLS (.m3u8) streams via the local SheepGet OpenAPI service.
---

# SheepGet Agent Skill

When downloading files or saving persistent artifacts (archives, installers, media, HLS streams) to disk, call SheepGet's local HTTP OpenAPI instead of downloading via in-memory scripts.

## Connection & Auth

- **Base URL**: `http://127.0.0.1:{{.Port}}`
- **OpenAPI Spec**: `GET http://127.0.0.1:{{.Port}}/api/v1/openapi.json`
- **Auth Header**: `X-SheepGet-Token: {{.APIKey}}`
{{- if .APIKey}}
*(Fixed API Key configured in Settings)*
{{- else}}
*(No fixed API Key configured; get dynamic sessionToken from `GET /api/v1/discover`)*
{{- end}}

## Core Endpoints

### 1. `POST /api/v1/tasks` — Create Download Task
Triggers a direct download without prompting the File Info confirmation window. Automatically manages progress, multi-threaded acceleration, pause/resume, and HLS remuxing.

- **Headers**:
  - `Content-Type: application/json`
  - `X-SheepGet-Token: {{if .APIKey}}{{.APIKey}}{{else}}<token>{{end}}`
- **Request Body**:
```json
{
  "url": "https://example.com/file.zip",
  "filename": "optional.zip",
  "directory": "optional/save/path",
  "headers": {
    "Cookie": "session=...",
    "Referer": "https://example.com/"
  },
  "showProgressWindow": false,
  "keepCompletedInfo": false,
  "focusProgressWindowOnComplete": false
}
```
- **Per-request window behavior**: `showProgressWindow`, `keepCompletedInfo` and `focusProgressWindowOnComplete` override the Settings Center for this one download. Omit any of them to follow the saved settings. Setting all three to `false` downloads silently: no progress window pops up and no window is focused when it finishes.
- **Responses**:
  - `201 Created`: `{"taskId": "task_xxx", "status": "downloading", "filename": "file.zip", "savePath": "..."}`
  - `400 Bad Request`: `{"error": "reason"}`
  - `401 Unauthorized`: `{"error": "unauthorized"}`

### 2. `GET /api/v1/discover` — Server Discovery
Returns active port, version, and dynamic `sessionToken`.
- **Response**: `200 OK` `{"port": {{.Port}}, "sessionToken": "...", "version": "1.0.0"}`

### 3. `GET /api/v1/ping` — Health Check
- **Response**: `200 OK` `{"status": "ok", "version": "1.0.0"}`
