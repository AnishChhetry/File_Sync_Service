# File Sync Service

File Sync Service keeps two directories in lockstep and provides a real-time dashboard that surfaces activity, status, and manual controls. The backend is written in Go (Gin-powered REST + WebSocket API), the frontend in React, and both communicate through the shared API surface.

## Architecture Overview

```
┌──────────┐   REST / WebSocket (Gin)   ┌────────────────────┐
│ React UI │ <────────────────────────► │ Go API Layer (Gin) │
└──────────┘                             └─────────┬──────────┘
                                                  │
                                        ┌─────────▼─────────────┐
                                        │      Sync Engine      │
                                        │  fsnotify, hashing,   │
                                        │  reconciliation, etc. │
                                        └─────────┬─────────────┘
                                  ┌───────────────┴───────────────┐
                                  │                               │
                      ./local_data (default root)      ./remote_data (default root)
                                  │                               │
                            Storage providers (filesystem, S3, GCS, ...)
```

## Project Structure

```
File Sync Service/
├── backend/
│   ├── cmd/                # Go entry point for the API server
│   ├── internal/
│   │   ├── api/            # Gin HTTP + WebSocket handlers
│   │   ├── engine/         # Sync engine orchestrating reconciliation
│   │   ├── models/         # Shared Go data structures
│   │   ├── storage/        # Pluggable storage provider implementations
│   │   └── config/         # Default filesystem roots and API port
│   ├── local_data/         # Default local watch folder
│   └── remote_data/        # Default remote folder mirror
├── frontend/
│   ├── src/                # React UI (dashboard, activity log, file list)
│   ├── public/             # Static assets for the frontend build
│   └── package.json        # Frontend dependencies and scripts
└── README.md
```

## Key Features

### Backend
- Bidirectional synchronization with SHA256-based change detection
- Storage-provider abstraction that defaults to the local filesystem but can be swapped for services like S3 or GCS
- Event-driven updates using `fsnotify`
- Conflict handling via modification timestamps
- Pause/resume and manual sync operations
- Adaptive worker pool (2–8 goroutines) with per-file locking for safe concurrent processing
- Event debouncing to collapse bursts of filesystem notifications
- REST endpoints and WebSocket event stream for external clients

#### Runtime flow
1. Ensure the configured local and remote roots exist, then build initial state maps from each provider.
2. Reconcile any divergences between providers before watching for changes.
3. Monitor both roots with `fsnotify`, enqueueing debounced events into a worker pool.
4. Process jobs concurrently while holding per-path locks, issuing sync/delete/conflict callbacks to the API layer.
5. Expose status, file listings, pause/resume, and manual sync controls through HTTP and WebSocket channels.

#### Configuration defaults
- Paths and port are defined in `backend/internal/config/constants.go` (@backend/internal/config/constants.go#1-8).
- Both roots are backed by the filesystem provider by default; swap in custom `storage.StorageProvider` implementations to connect to services like S3 or GCS.
- Manual sync requests are rejected while the engine is paused, ensuring consistent reconciliation state.

### Frontend
- Live dashboard with file counts, activity feed, and controls
- WebSocket-driven event log without polling overhead
- Responsive layout suitable for desktop and tablet displays

## Getting Started

### Prerequisites
- Go 1.24 or later
- Node.js 18+ and npm

### Backend
```bash
cd backend
go mod download
go run cmd/main.go
```

On startup the server instantiates filesystem-backed providers rooted at the configured paths (default `./local_data` and `./remote_data`), ensures those folders exist, and exposes HTTP/WebSocket APIs on port `8080` via Gin. Runtime logs surface reconciliation progress, watcher activity, and API actions.

To experiment with an alternate backend, implement the `storage.StorageProvider` interface (build state map, read/write streams, metadata, deletes, ensure directory, path helpers) and wire it into `engine.NewSyncEngine`. Both `./local_data` and `./remote_data` are simply the default filesystem roots; you can replace either or both with custom providers (e.g., S3, GCS, in-memory) without changing the higher layers.

### Frontend
```bash
cd frontend
npm install
npm start
```

The dashboard is available at `http://localhost:3000`.

## Development

```bash
# Backend
go test ./...

# Frontend
npm test
npm run build
```

## API Summary

| Endpoint        | Method | Description                  |
|-----------------|--------|------------------------------|
| `/api/status`   | GET    | Sync engine status snapshot  |
| `/api/files`    | GET    | Consolidated file list       |
| `/api/pause`    | POST   | Pause automatic sync         |
| `/api/resume`   | POST   | Resume automatic sync        |
| `/api/sync`     | POST   | Trigger manual reconciliation|
| `/ws`           | WS     | Streaming sync events        |


