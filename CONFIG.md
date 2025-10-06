# MapRelay Server Configuration

The server uses a JSON config file to control authentication and allowed programs.

## Example `server_config.json`

```json
{
  "password": "change-me",
  "baseGamePath": "C:/Program Files (x86)/Steam/steamapps/common/GarrysMod",
  "gamedir": "garrysmod",
  "winePath": "wine",
  "programs": {}
}
```

### Fields

- **password**: If set, clients must provide this value to access protected endpoints.
- **baseGamePath**: Path to the base game installation containing Source tools.
- **gamedir**: Absolute path or folder name under `baseGamePath` for the target game.
- **winePath**: Optional Wine command for Linux (default: `wine`).
- **programs**: Mapping of program names to absolute paths. Presets must only reference names listed here.

### Tool Path Defaults

If not set in `programs`, the server will auto-derive tool paths from `baseGamePath`:
- vbsp: `<baseGamePath>/bin/win64/vbsp.exe`
- vvis: `<baseGamePath>/bin/win64/vvis.exe`
- vrad: `<baseGamePath>/bin/win64/vrad.exe`

### Linux Notes

On Linux, `.exe` programs are run with `wine` (or `winePath` if set).

### Presets Store

Presets are stored in a JSON file (see `-presets` flag). All referenced programs must be in the config allow-list.

### Authentication

- Password is required for modifying presets and triggering compiles if set.


