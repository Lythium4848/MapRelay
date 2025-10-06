# MapRelay

MapRelay is a lightweight client/server bridge for Source engine map compilation using shareable, editable presets.

## Features

- Password-protected server
- Configurable, allow-listed programs
- JSON presets system
- WebSocket live compile stream
- Simple HTTP API for presets

## Installation

```sh
git clone https://github.com/Lythium4848/MapRelay
cd MapRelay
go build -o maprelay
```

## Usage

### Start Server

```sh
./maprelay -server -port 8000 -config server_config.json -presets presets.json
```

### Run Client (compile)

```sh
./maprelay -client \
  -server localhost:8000 \
  -vmf path/to/map.vmf \
  -preset default \
  -password change-me
```

### Upload Preset

```sh
./maprelay -client \
  -server localhost:8000 \
  -uploadPreset preset.json \
  -password change-me
```

## Preset Example

```json
{
  "name": "default",
  "steps": [
    {"program": "vbsp", "args": ["-game", "$gamedir", "$vmf"]},
    {"program": "vvis", "args": ["-fast", "-game", "$gamedir", "$bsp"]},
    {"program": "vrad", "args": ["-game", "$gamedir", "$bsp"]}
  ]
}
```

## API

- `GET /api/presets` — List presets
- `POST /api/presets` — Add/update preset (requires password)

## License

MIT

## Configuration

See [CONFIG.md](CONFIG.md) for server configuration details.
