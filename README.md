# AppDeck

AppDeck is a local-first navigation deck for self-hosted apps. It scans your
`~/apps` folder, Docker containers, and user systemd services, then merges that
machine state with your own layout preferences.

The result is a small web dashboard at `http://127.0.0.1:8788` with draggable
groups, searchable app cards, and a JSON config you can back up or move to
another machine.

## Features

- Scans `~/apps` for app folders, README files, package metadata, and compose ports.
- Reads Docker containers and compose labels to find running services and exposed ports.
- Reads selected user systemd services, including local services such as `go-music-dl`.
- Saves user edits, hidden apps, manual apps, sorting, and categories to JSON.
- Ships as a single Go binary with embedded HTML/CSS/JS.
- Listens on `127.0.0.1` by default.

## Quick Start

```bash
go build -o appdeck ./cmd/appdeck
./appdeck
```

Open:

```text
http://127.0.0.1:8788
```

## Configuration

Default config file:

```text
~/.config/appdeck/appdeck.json
```

Default scanned app root:

```text
~/apps
```

If `~/.config/appdeck/appdeck.json` does not exist and `~/apps/apps-nav.json`
does exist, AppDeck imports the old navigation file as initial preferences.

## Run With systemd

Example user service:

```ini
[Unit]
Description=AppDeck local navigation
After=network-online.target

[Service]
Type=simple
WorkingDirectory=/home/kamjin/projects/app-deck
ExecStart=/home/kamjin/projects/app-deck/appdeck --host 127.0.0.1 --port 8788
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
```

Then:

```bash
systemctl --user daemon-reload
systemctl --user enable --now appdeck.service
```

## API

- `GET /api/apps` returns the merged catalog.
- `POST /api/preferences` saves preferences.
- `POST /api/rescan` rescans apps, Docker, and systemd.
- `GET /api/export` exports preferences JSON.
- `POST /api/import` imports preferences JSON.

## License

MIT
