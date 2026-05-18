# gowitness-new

Fork of [gowitness](https://github.com/sensepost/gowitness) v3.1.1 with a built-in review system for triaging screenshots during bug bounty and penetration testing engagements.

![Review tags and infinite scroll gallery](https://img.shields.io/badge/gowitness-v3.1.1-blue) ![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white) ![Node](https://img.shields.io/badge/Node-18+-339933?logo=node.js&logoColor=white)

## What's Added

**Review tags** — each card in the gallery has a row of one-click status buttons:

| Tag | Color | Purpose |
|-----|-------|---------|
| Done | Green | Reviewed, nothing interesting |
| Attention | Red | Needs a closer look |
| Interesting | Yellow | Worth investigating |
| Vuln | Purple | Confirmed vulnerability |
| Junk | Gray | Noise (card fades out) |

Clicking an active tag removes it. A colored left border indicates the current status.

**Comments** — a text field under every card with autosave (0.8s debounce).

**Filter pills** — toolbar shows counts per status. Click to filter the gallery by tag.

**Infinite scroll** — no pagination, just scroll down. Loads 48 results per batch automatically.

**Detail page** — clicking a screenshot opens the detail view with review controls at the top.

**Export** — `GET /api/review/export` returns a markdown summary of all tagged and commented hosts.

Everything else (scanning, probing, nmap import, etc.) works exactly like upstream gowitness.

## Install

Download the binary from [Releases](../../releases) or build from source (see below).

```bash
# copy to PATH
sudo cp gowitness-new /usr/local/bin/gowitness-new
```

## Usage

```bash
# from the directory with gowitness.sqlite3 and screenshots/
gowitness-new report server

# with explicit paths
gowitness-new report server \
  --db-uri "sqlite://gowitness.sqlite3" \
  --screenshot-path ./screenshots \
  --port 7171
```

Open `http://127.0.0.1:7171` in your browser.

All standard gowitness commands work as usual:

```bash
gowitness-new scan single --url https://example.com
gowitness-new scan nmap -f nmap.xml
gowitness-new scan file -f urls.txt
```

## API

```
GET  /api/review/stats             stats per status
GET  /api/review/export            markdown export of tagged/commented hosts
GET  /api/review/{id}              get review for a result
POST /api/review/{id}              {"status":"attention","comment":"text"}
POST /api/review/bulk              {"ids":[1,2,3],"status":"junk"}
GET  /api/results/gallery?review=  filter: done|attention|interesting|vuln|junk|unseen|commented
```

Valid `status` values: `done`, `attention`, `interesting`, `vuln`, `junk`, or empty string to clear.

## Data Storage

A `reviews` table is created automatically in the same gowitness database. Original gowitness data is never modified.

## Build from Source

```bash
git clone https://github.com/dhudhsgs93-arch/gowitness-new.git
cd gowitness-new

# build frontend
cd web/ui && npm install && npm run build && cd ../..

# build binary
CGO_ENABLED=0 go build -o gowitness-new .
```

Requirements: Go 1.21+, Node 18+.

## License

Same as [gowitness](https://github.com/sensepost/gowitness) — GPL-3.0.
