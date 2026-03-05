# Golinky 🔗

A simple URL shortener service, inspired by [Tailscale Golink](https://github.com/tailscale/golink) but without authentication.

Perfect for internal networks, home labs, or team environments where you need quick, memorable shortcuts.

## Features

- Modern, responsive interface
- Statistics tracking
- Advanced URL templating support
- SQLite persistence
- Export links and restore via JSON file
- Kubernetes ready

## Quick Start

### Docker Compose (standalone)

```bash
docker compose up -d
```

Uses [`docker-compose.yaml`](docker-compose.yaml):

```yaml
services:
  golinky:
    image: gcr.io/didactiklabs/golinky:latest
    environment:
      - TZ=Europe/Paris
    volumes:
      - golinky-data:/app/data
    ports:
      - "8080:8080"
    restart: unless-stopped
```

### Docker Compose with Netbird

For exposing golinky on a [Netbird](https://netbird.io) mesh network (accessible as `go/` on your tailnet-like overlay):

```bash
# Set your Netbird setup key
export NB_SETUP_KEY=your-setup-key

# Optionally set your management URL (defaults to https://api.netbird.io:443)
export NB_MANAGEMENT_URL=https://your-netbird-mgmt:443

docker compose -f docker-compose-netbird.yaml up -d
```

### Kubernetes (with optional Netbird)

Kubernetes manifests are located in the [`deploy/`](deploy/) directory. Apply them with:

```bash
kubectl apply -f deploy/
```

> **Note:** To unable netbird integration, uncomment some manifests in the `deploy/` directory.


### From source

```bash
go build -o golinky
./golinky
```

By default golinky listens on `localhost:8080` and stores data in `./golinky.db`.

```bash
# Custom path for db
./golinky -listen=:8080 -sqlitedb=/path/to/database.db

# Set timezone
TZ="Europe/Paris" ./golinky
```

## Usage

### Creating a link

1. Open `http://localhost:8080`
2. Enter a short name (e.g. `gh`) and the destination URL (e.g. `https://github.com`)
3. Click **Create**
4. Access it at `http://localhost:8080/gh`

### Pages

| Route | Description |
|-------|-------------|
| `/` | Home — create links & view popular links |
| `/.all` | Browse and search all links |
| `/.help` | Help & advanced options documentation |
| `/.export` | Download all links in JSON Lines format |
| `/.detail/{name}` | View / edit a specific link |
| `/.delete/{name}` | Delete a link |

### Advanced URL templates

Destination URLs support [Go template syntax](https://pkg.go.dev/text/template). Available data and functions:

| Field / Function | Description |
|------------------|-------------|
| `{{.Path}}` | Remaining path after the short name |
| `{{.Now}}` | Current date/time (`time.Time`) |
| `{{QueryEscape .Path}}` | URL query-encode the path |
| `{{PathEscape .Path}}` | URL path-encode the path |
| `{{ToLower .Path}}` | Convert to lowercase |
| `{{ToUpper .Path}}` | Convert to uppercase |
| `{{TrimPrefix .Path "p"}}` | Remove a prefix |
| `{{TrimSuffix .Path "s"}}` | Remove a suffix |
| `{{Match "pattern" .Path}}` | Regex match |

**Example — search engine:**

```
short: g
long:  https://www.google.com/{{if .Path}}search?q={{QueryEscape .Path}}{{end}}
```

`go/g/pangolins` → `https://www.google.com/search?q=pangolins`

See `/.help` for more examples.

## API

```bash
# Create / update a link
curl -X POST http://localhost:8080/ -d "short=example" -d "long=https://example.com"

# Get link details (JSON)
curl http://localhost:8080/.detail/example -H "Accept: application/json"

# Export all links (JSON Lines)
curl http://localhost:8080/.export > backup.json

# Delete a link
curl -X POST http://localhost:8080/.delete/example
```

## Backup & Restore

```bash
# Export
curl http://localhost:8080/.export > backup-$(date +%Y%m%d).json

# Restore on startup (only imports links that don't already exist)
./golinky -snapshot=backup.json
```

## Development

```bash
go mod download
go build -o golinky
./golinky -listen=localhost:8080
```

You can also use the docker-compose file for dev:
```bash
docker compose --file docker-compose-local.yaml up -d
```

## License

BSD-3-Clause
