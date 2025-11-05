# File Sync Service

File Sync Service keeps two directories in lockstep and provides a real-time dashboard that surfaces activity, status, and manual controls. The backend is written in Go, the frontend in React, and both communicate through REST and WebSocket APIs.

## Architecture Overview

```
┌──────────┐      REST / WebSocket      ┌──────────────┐
│ React UI │ <────────────────────────► │ Go API Layer │
└──────────┘                            └──────┬───────┘
                                               │
                                       ┌───────▼────────┐
                                       │ Sync Engine    │
                                       │ (fsnotify,     │
                                       │  hashing, etc.)│
                                       └───────┬────────┘
                               ┌───────────────┼───────────────┐
                               │               │               │
                      ./local_data     ./remote_data      Storage providers
```

## Project Structure

```
File Sync Service/
├── backend/
│   ├── cmd/                # Go entry point for the 
│   ├── internal/
│   │   ├── api/            # HTTP + WebSocket server
│   │   ├── engine/         # Sync engine orchestrating 
│   │   └── models/         # Shared Go data structures
│   │   └── storage/        # Pluggable storage 
│   ├── local_data/         # Default local watch 
│   └── remote_data/        # Default remote folder 
├── frontend/
│   ├── src/                # React UI (dashboard, activity log, file list)
│   ├── public/             # Static assets for the 
│   └── package.json        # Frontend dependencies and 
└── README.md
```

## Key Features

### Backend
- Bidirectional synchronization with SHA256-based change detection
- Storage-provider abstraction that defaults to the local filesystem but can be swapped for services like S3 or GCS
- Event-driven updates using `fsnotify`
- Conflict handling via modification timestamps
- Pause/resume and manual sync operations
- REST endpoints and WebSocket event stream for external clients

### Frontend
- Live dashboard with file counts, activity feed, and controls
- WebSocket-driven event log without polling overhead
- Responsive layout suitable for desktop and tablet displays

## Getting Started

### Prerequisites
- Go 1.21 or later
- Node.js 18+ and npm

### Backend
```bash
cd backend
go mod download
go run cmd/main.go
```

The server instantiates a local `FileSystemProvider`, watches `./local_data` and `./remote_data`, and exposes HTTP/WebSocket APIs on port `8080`.

To experiment with an alternate backend, implement the `storage.StorageProvider` interface (build state map, read/write streams, metadata, deletes, ensure directory, path) and wire it into `engine.NewSyncEngine`.

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

## Roadmap

- Add simple ignore rules so you can skip syncing certain files or folders.
- Record past sync actions and make it easy to undo a change when needed.
- Support more source/target pairs, including cloud storage providers.
- Introduce user accounts with basic permissions.
- Enhance the dashboard with richer file previews and quick actions.

## License

MIT License
