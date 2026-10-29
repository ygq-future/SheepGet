---
name: sheep-get
description: Download files, media, HLS (.m3u8) streams, or many related small files (model packs, app assets, datasets, dependencies) as a single zip bundle via the local SheepGet OpenAPI service.
---

# SheepGet Agent Skill

Use SheepGet's local HTTP OpenAPI (`http://127.0.0.1:{{.Port}}`) instead of in-memory scripts whenever downloading files, media, HLS (`.m3u8`) streams, or multi-file bundles to disk.

## Auth & Endpoints

- **Header**: `X-SheepGet-Token: {{if .APIKey}}{{.APIKey}}{{else}}<sessionToken>{{end}}`
{{- if .APIKey}} *(Fixed API Key configured in Settings)*
{{- else}} *(No fixed API Key configured; get dynamic `sessionToken` from `GET /api/v1/discover`)*
{{- end}}
- **`GET /api/v1/discover`** → `{"port": {{.Port}}, "sessionToken": "...", "version": "..."}`
- **`GET /api/v1/ping`** → `{"status": "ok", "version": "..."}`
- **`GET /api/v1/openapi.json`** → OpenAPI 3.0 spec

## `POST /api/v1/tasks` — Create Download or Zip Member

Starts a direct download (multi-threaded, pause/resume, HLS remuxing) without the confirmation dialog, or registers a member into a zip batch.

```json
{
  "url": "https://example.com/file.zip",
  "filename": "optional.zip",
  "directory": "optional/save/path",
  "headers": { "Cookie": "session=...", "Referer": "https://example.com/" },
  "showProgressWindow": false,
  "keepCompletedInfo": false,
  "focusProgressWindowOnComplete": false
}
```

- **Window flags**: `showProgressWindow`, `keepCompletedInfo`, `focusProgressWindowOnComplete` override Settings for this request (omit to use saved defaults; set all `false` for silent download).
- **Responses**: `201 Created` (`{"taskId": "task_xxx", "status": "downloading", ...}` or `{"taskId": "", "zipTaskId": "...", "zip": {...}}`), `400 Bad Request`, `401 Unauthorized`, `409 Conflict`.

## Zip Aggregation (MUST for 3+ related files)

When downloading **multiple files for the same model, app, dataset, or runnable scenario**, you **MUST** aggregate them into **one** zip batch (`zipId`) instead of creating one task per file. Do **not** use zip aggregation for a single standalone file, unrelated files, HLS streams, or when the user explicitly requests separate archives.

### Batch boundary: usage scenario, not source or file type

Group by **what the user uses together**:
- **Different hosts are still one batch**: HuggingFace weights + GitHub release runtime + CDN assets for the same model/app MUST share one `zipId`.
- **Model + runtime/dependencies = one batch**: Anything needed to run the target belongs in the same batch.
- **Already-zipped or large companion files stay in the batch**: Put `.zip` dependencies inside the outer zip as a nested archive (double extraction is fine), and include large weight files when they have companions.
- **Never create a second zip task** for the same application/model/scenario unless the user explicitly asks for separate archives. If intent is ambiguous, ask before creating a second zip task.

### Workflow & Rules

1. **Plan before sealing**: There is no cancel/delete API and `"zipFinal": true` is irreversible. Enumerate every file across all hosts first; if a missing dependency is found after sealing, do not silently start a second task—inform the user and ask.
2. **Register each member** via `POST /api/v1/tasks` with:
   - `zipId`, `zipName` (`<zipName>.zip`, fixed with `directory` by the first member; later members may omit but must not change them), and its own `headers` (**not** inherited from the first member).
   - `zipPath`: Relative `/` path inside the zip (no `..`, drive letters, absolute paths, or Windows reserved names). **Never rename members** to work around `409` collisions; model/app files must keep exact names.
   - `zipFinal`: `false` until the **last** member (`"zipFinal": true`), which seals the batch and starts the download immediately (no polling needed). Retry failed member probes before sealing; `409` after sealing means the batch is closed.

### Example: Cross-Host Model + Nested Runtime (ONE Batch)

User: *"下载 Breeze-TTS-2 模型和它运行需要的依赖"* → Send all members (`model.gguf`, `ref_voice.wav`, `README.md` from HuggingFace + runtime `.zip` from GitHub) under the same `zipId`, setting `"zipFinal": true` only on the last request:

```json
// Member 1..N-1 (HuggingFace files)
{
  "url": "https://huggingface.co/org/Breeze-TTS-2/resolve/main/model.gguf",
  "zipId": "breeze-tts-2--20261029",
  "zipName": "Breeze-TTS-2",
  "zipPath": "Breeze-TTS-2/model.gguf",
  "headers": { "Authorization": "Bearer ..." },
  "zipFinal": false
}
// Final Member N (GitHub release zip nested inside the same batch)
{
  "url": "https://github.com/org/Breeze-TTS-2/releases/download/v0.1.3/breeze-tts-2-win-x64.zip",
  "zipId": "breeze-tts-2--20261029",
  "zipName": "Breeze-TTS-2",
  "zipPath": "Breeze-TTS-2/runtime/breeze-tts-2-win-x64.zip",
  "zipFinal": true
}
```
