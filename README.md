# MapRelay

MapRelay is a lightweight client/server bridge for (mainly) source engine map compilation using shareable, editable presets. It features:

- A password-protected server with configurable, allow‑listed programs.
- A JSON “presets” system that defines what programs to run and with what args.
- A WebSocket stream used by the client to request a compile and receive live step messages.
- Simple HTTP endpoints to list and update presets.

This repository contains a single binary with two subcommands: `-server` and `-client`.


## Table of contents
- Overview and architecture
- Requirements
- Build and install
- Server
  - Flags
  - Configuration file
  - Presets store
  - HTTP API
  - WebSocket protocol
- Client
  - Flags
  - Typical workflows
- Presets format
- Logging
- Security notes
- Development


## Overview and architecture

- main.go: Entrypoint that initializes logging and dispatches to server or client based on the first CLI arg.
- server/:
  - server.go: HTTP server, WebSocket handler, and wiring.
  - config.go: Loads a JSON config with allowed programs and an optional password.
  - presets.go: In‑process presets store backed by a JSON file, plus HTTP handlers.
- client/:
  - client.go: CLI for either uploading presets (HTTP) or triggering a compile (WebSocket).
- logging/:
  - logging.go: Global, prefixed Zap logger setup used by both client and server.

High level flow:
1) Server starts with a config that specifies which programs are allowed and an optional password.
2) Presets are stored as JSON (server-side file); client can upload/update them.
3) Client connects via WebSocket and sends a compile request with VMF path, preset name, and password.
4) Server validates password and preset, then streams step messages to client, ending with `COMPILE_DONE`.


## Requirements
- Go (per go.mod directive). Build has been verified via `go build ./...`.


## Build and install
```
# Clone and build
git clone https://github.com/Lythium4848/MapRelay
cd MapRelay
go build ./...

# Build single binary (optional)
go build -o maprelay
```

Run without args to see usage:
```
./maprelay
Usage: program -client | -server [args...]
```


## Server

Start the server subcommand:
```
./maprelay -server -port 8000 -config server_config.json -presets presets.json
```

### Flags
- -port string: Port to listen on (default "8000").
- -config string: Path to server config JSON (default "server_config.json").
- -presets string: Path to presets JSON store (default "presets.json").

### Configuration file
`server/config.go` loads a JSON configuration that controls authentication and which programs can be referenced by presets.

Example `server_config.json`:
```json
{
  "password": "change-me",
  "baseGamePath": "C:/Program Files (x86)/Steam/steamapps/common/GarrysMod",
  "gamedir": "garrysmod",
  "winePath": "wine",
  "programs": {}
}
```
- password: If set, clients must provide this value either via the WebSocket compile request or as `X-Password` when modifying presets through HTTP.
- baseGamePath: Path to the base game installation that contains the Source tools (e.g., `C:/Program Files (x86)/Steam/steamapps/common/GarrysMod`). Used to resolve tool paths; the server will auto-derive default tool paths when not explicitly set:
  - vbsp: `<baseGamePath>/bin/win64/vbsp.exe`
  - vvis: `<baseGamePath>/bin/win64/vvisplusplus.exe`
  - vrad: `<baseGamePath>/bin/win64/vrad.exe`
- gamedir: Either an absolute path to the target game folder (the one containing `gameinfo.txt`) or a short folder name under `baseGamePath` (e.g., `garrysmod` or `hl2`). The server will resolve a non-absolute `gamedir` by joining it with `baseGamePath`. The server never auto-discovers the game directory from the VMF path; you must set it explicitly.
- winePath: Optional path or command name for Wine on Linux (defaults to `wine`). Only affects how commands are run/shown on Linux.
- programs: A mapping of friendly program names to absolute paths. Presets must only reference names listed here. Any entries provided here override the derived defaults from `baseGamePath`. 

Linux notes:
- On Linux, if a program ends with `.exe`, the server will prefix it with `wine` (or `winePath` if set) when running/showing the command.

### Presets store
Presets are persisted in a JSON file specified by `-presets`. The server loads and serves them and enforces that all referenced programs are in the config allow‑list.

### HTTP API
- GET /api/presets
  - Returns the full list of presets as JSON.
- POST /api/presets (also accepts PUT)
  - Headers: `Content-Type: application/json`, `X-Password: <password>`.
  - Body: A single preset JSON object (see Presets format below).
  - Creates a new preset or updates an existing one (by name).
  - On auth failure, server responds with 401/403 equivalent and message `AUTH_FAILED`.
  - On success, HTTP 200.

### WebSocket protocol
- URL: `ws://<host>:<port>/` (root path)
- Client sends a single JSON message immediately after connecting:
```json
{
  "vmf": "map.vmf",
  "preset": "default",
  "password": "change-me"
}
```
- Server validates the password and ensures the preset exists. It then streams typed JSON messages, for example:
  - `{ "type": "info", "message": "Starting compile..." }`
  - `{ "type": "vbsp", "message": "... tool output ..." }` (tool-prefixed stdout/stderr)
  - `{ "type": "bsp", "name": "map.bsp", "data": "<base64>" }` (compiled BSP payload)
  - `{ "type": "done" }` (signals completion)


## Client

Trigger a compile over WebSocket:
```
./maprelay -client \
  -server localhost:8000 \
  -vmf path/to/map.vmf \
  -preset default \
  -password change-me
```

Upload or update a preset via HTTP and exit:
```
./maprelay -client \
  -server localhost:8000 \
  -uploadPreset preset.json \
  -password change-me
```

### Flags
- -server string: Server WebSocket URL (default `ws://localhost:8000`).
- -api string: Server HTTP base URL (default `http://localhost:8000`).
- -vmf string: VMF path (string included in the request; server-side handling is customizable).
- -preset string: Preset name to use (default `default`).
- -password string: Server password, if configured.
- -uploadPreset string: Path to a preset JSON file to upload to the server. If set, the client uploads the file and exits.


## Presets format
A preset is a named list of steps. Each step references an allowed program (defined in the server config) and its arguments array.

Example preset object:
```json
{
  "name": "default",
  "steps": [
    {"program": "vbsp", "args": ["-game", "/path/to/game", "${vmf}"]},
    {"program": "vvis", "args": ["-fast"]},
    {"program": "vrad", "args": ["-final"]}
  ]
}
```
Notes:
- The server validates that every `program` listed exists in the config `programs` map.
- You can design your own argument conventions (e.g., `${vmf}` placeholder). Current sample server flow echoes arguments back as text; extend execution as needed.


## Logging
Logging is provided by Uber Zap and initialized globally in `main.go` via `logging.Init()`.
- Use `logging.Named("Server")` or `logging.Named("Client")` for component-prefixed loggers.
- Set `APP_ENV=development` to use Zap’s development encoder (more human-readable).
- In production (default), structured JSON logs are emitted.


## Security notes
- Set a strong `password` in your server config and keep it secret. The server will reject:
  - WebSocket compile requests with incorrect `password`.
  - Preset modifications without the correct `X-Password` header.
- Consider running the server behind TLS (e.g., via a reverse proxy) if used across networks.
- Ensure only trusted programs are listed in `programs` and that their paths are correct.


## Development
- Build: `go build ./...`
- Run tests: (no tests yet; recommended to add unit tests for config loading, preset validation, and API handlers.)
- Code layout mirrors simple package separation for server/client/logging.

Contributions: PRs and issues welcome.
