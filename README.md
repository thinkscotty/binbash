# binbash

A simple, fast, self-hosted home inventory app for tracking what's in your storage bins — built for speed of entry and dead-simple search, not for feature sprawl.

binbash is a single, self-contained binary. There's no database server to run, no runtime to install, and no build step required — download one file, drop a small config next to it, and start it.

## Contents

- [What you need](#what-you-need)
- [Install](#install)
  - [1. Download](#1-download)
  - [2. Verify the download (optional)](#2-verify-the-download-optional)
  - [3. Extract](#3-extract)
  - [4. Create a config file](#4-create-a-config-file)
  - [5. Run it](#5-run-it)
  - [6. Sign in](#6-sign-in)
- [Configuration](#configuration)
  - [The config file](#the-config-file)
  - [Every setting](#every-setting)
  - [Overriding with environment variables](#overriding-with-environment-variables)
- [AI tagging](#ai-tagging)
- [Backups](#backups)
- [Running as a service (systemd)](#running-as-a-service-systemd)
- [Running with Docker](#running-with-docker)
- [Put a reverse proxy in front](#put-a-reverse-proxy-in-front)
- [Building from source](#building-from-source)
- [License](#license)

## What you need

- A computer or server to run it on — Linux, macOS, or Windows, on either Intel/AMD (`amd64`) or ARM (`arm64`, e.g. a Raspberry Pi 4/5 or an ARM VPS).
- Nothing else. The binary has no dependencies.

binbash listens on plain HTTP behind a single shared password. That's fine on your home network or a private/VPN-only host, but **don't expose it directly to the internet** — see [Put a reverse proxy in front](#put-a-reverse-proxy-in-front).

## Install

### 1. Download

Go to the [**Releases page**](https://github.com/thinkscotty/binbash/releases) and download the archive for your platform:

| Your machine | Download |
|---|---|
| Linux, Intel/AMD 64-bit | `binbash-<version>-linux-amd64.tar.gz` |
| Linux, ARM 64-bit (Raspberry Pi 4/5, ARM VPS) | `binbash-<version>-linux-arm64.tar.gz` |
| macOS, Apple Silicon (M1/M2/M3/M4) | `binbash-<version>-darwin-arm64.tar.gz` |
| macOS, Intel | `binbash-<version>-darwin-amd64.tar.gz` |
| Windows, 64-bit | `binbash-<version>-windows-amd64.zip` |

Or download from the terminal (set `VERSION` to the current release shown on the Releases page):

```sh
VERSION=v0.1.0   # <-- check the Releases page for the latest version
curl -LO "https://github.com/thinkscotty/binbash/releases/download/$VERSION/binbash-$VERSION-linux-amd64.tar.gz"
```

### 2. Verify the download (optional)

Each release includes a `SHA256SUMS.txt`. If you'd like to confirm your download wasn't corrupted or tampered with, grab it and check:

```sh
curl -LO "https://github.com/thinkscotty/binbash/releases/download/$VERSION/SHA256SUMS.txt"

# Linux:
sha256sum -c SHA256SUMS.txt --ignore-missing

# macOS:
shasum -a 256 -c SHA256SUMS.txt --ignore-missing
```

You want to see `OK` next to the file you downloaded.

### 3. Extract

```sh
tar xzf binbash-$VERSION-linux-amd64.tar.gz
cd binbash-$VERSION-linux-amd64
```

On Windows, unzip the `.zip` and open a terminal in the extracted folder. Each archive contains the `binbash` binary and the license; recent releases also include a fully-commented `binbash.example.toml` and a sample `deploy/binbash.service` you can copy instead of writing them from scratch.

**macOS note:** the binary is unsigned, so the first time you run it Gatekeeper may block it. Clear the download quarantine flag once and you're set:

```sh
xattr -d com.apple.quarantine binbash
```

### 4. Create a config file

binbash reads its settings from a `binbash.toml` file sitting next to the binary. Create one now — at minimum it needs a login password (8 characters or more):

```toml
# binbash.toml
password = "change-this-to-something-strong"
```

That's genuinely all you need to start. Every other setting is optional and covered in [Configuration](#configuration) below.

> **Why a file and not just environment variables?** A config file is easy to keep, back up, and edit later without remembering a string of `export` commands — and it's the recommended way to run binbash. Environment variables still work and still take precedence when set, which is handy for one-off overrides (see [Overriding with environment variables](#overriding-with-environment-variables)).

### 5. Run it

```sh
./binbash
```

You should see:

```
config: loaded ./binbash.toml
binbash listening on :8080
```

The SQLite database is created automatically at `./data/binbash.db` on first run.

### 6. Sign in

Open <http://localhost:8080> (or `http://<server-ip>:8080` from another device on your network) and sign in with the password from your config file.

That password only **bootstraps** your account on first run. Change it any time from **Account → Change password** in the app — from then on the database is the source of truth, and editing `password` in the file again won't change your login.

## Configuration

### The config file

By default binbash looks for `binbash.toml` in the directory you run it from. To keep the file somewhere else (or name it something else), point `BINBASH_CONFIG` at it:

```sh
BINBASH_CONFIG=/etc/binbash/config.toml ./binbash
```

Here's a complete, annotated config file. Every field is optional except `password`, and any field you leave out falls back to its default:

```toml
# ---- Core ----

# Login password used to bootstrap your account on first run (min 8 chars).
# Required: binbash won't start without one, here or via BINBASH_PASSWORD.
password = "change-this-to-something-strong"

# HTTP port to listen on.
port = "8080"

# Where the SQLite database lives. The parent directory is created for you.
db_path = "./data/binbash.db"

# Directory for automatic CSV backups. Empty = disabled. See "Backups" below.
auto_backup_dir = ""

# ---- AI tagging (optional) ----
# Setting base_url is what turns AI tagging on. Leave it empty to keep the
# feature off entirely. See "AI tagging" below.
[ai]
base_url    = ""          # OpenAI-compatible endpoint, e.g. https://api.openai.com/v1
api_key     = ""          # API key for that endpoint
model       = ""          # model name to request
tag_count   = 3           # max tags suggested per item (0–8)
tag_breadth = "moderate"  # how related suggestions should be: narrow | moderate | broad
```

### Every setting

| Config file | `[ai]` table? | Default | Purpose |
|---|---|---|---|
| `password` | no | *(required)* | Initial login password (8–72 characters). Bootstraps the account on first run; change it in-app afterward. |
| `port` | no | `8080` | HTTP listen port. |
| `db_path` | no | `./data/binbash.db` | SQLite database file location. Its parent directory is created automatically. |
| `auto_backup_dir` | no | *(empty = disabled)* | Directory to write automatic CSV backups to. |
| `base_url` | yes | *(empty = AI off)* | OpenAI-compatible endpoint. Include any path prefix the provider needs (e.g. `https://api.openai.com/v1`) — `/chat/completions` is appended to it. |
| `api_key` | yes | *(empty)* | API key for the AI endpoint. |
| `model` | yes | *(empty)* | Model name to request. |
| `tag_count` | yes | `3` | Max AI-suggested tags per item (`0`–`8`; `0` keeps the feature on but generates no tags). |
| `tag_breadth` | yes | `moderate` | How closely related suggested tags should be: `narrow`, `moderate`, or `broad`. |

### Overriding with environment variables

Every setting also has a `BINBASH_*` environment variable, and **environment variables always win over the config file**. That precedence — defaults < config file < environment — lets you keep everything in the file but override a single value at runtime without editing it. This is mainly useful for containers (Docker's `-e`) or quick tests.

| Environment variable | Overrides |
|---|---|
| `BINBASH_CONFIG` | *(not in the file)* — path to the config file itself. |
| `BINBASH_PASSWORD` | `password` |
| `BINBASH_PORT` | `port` |
| `BINBASH_DB_PATH` | `db_path` |
| `BINBASH_AUTO_BACKUP_DIR` | `auto_backup_dir` |
| `BINBASH_AI_BASE_URL` | `[ai].base_url` |
| `BINBASH_AI_API_KEY` | `[ai].api_key` |
| `BINBASH_AI_MODEL` | `[ai].model` |
| `BINBASH_AI_TAG_COUNT` | `[ai].tag_count` |
| `BINBASH_AI_TAG_BREADTH` | `[ai].tag_breadth` |

You can run binbash with **no config file at all** and configure it entirely through these variables if you prefer — the file is the recommended path, not a required one.

## AI tagging

AI tagging is optional and off until you set `[ai].base_url` (plus `api_key` and `model` as your provider requires). Once configured, the **Items** page shows a "Tag up to 25 items" button. It asks the AI for search-friendly keyword suggestions — synonyms, alternate names, categories — for items that haven't been tagged yet, and **appends** them to each item's existing keywords. It never edits or removes what's already there.

Once an item is successfully tagged it's skipped by future runs. An item whose response came back with no usable tags is left untagged so a later run retries it, rather than being marked done with nothing. Click the button again to work through a larger backlog in batches of 25.

Any OpenAI-compatible endpoint works, including reasoning models (Qwen3, DeepSeek-R1, and the like). Those spend part of their reply "thinking" before answering, so binbash requests a generous token budget to let them finish and still return usable tags.

## Backups

Visit **Backup** in the nav to download a CSV of your whole inventory (one row per item, with its bin's name, category, and description), or to import a CSV back in. Importing adds to what you already have by default; check **Replace existing inventory** to wipe and reload from the file instead. The search page nudges you to back up once you've added 50+ items since the last one.

To automate it, set `auto_backup_dir` to a writable directory. binbash then writes one of these CSVs automatically once that threshold is crossed, keeping the 5 most recent.

## Running as a service (systemd)

On a Linux server you'll usually want binbash to start on boot and restart if it crashes. A sample unit is in the repo at [`deploy/binbash.service`](deploy/binbash.service).

```sh
# Create a dedicated, unprivileged user and an install directory
sudo useradd -r -s /usr/sbin/nologin binbash
sudo mkdir -p /opt/binbash/data

# Install the binary (from your extracted download)
sudo cp binbash /opt/binbash/

# Create the config file
sudo tee /opt/binbash/binbash.toml >/dev/null <<'EOF'
password = "change-this-to-something-strong"
port = "8080"
db_path = "./data/binbash.db"
EOF

# Hand everything to the binbash user and install the service
sudo chown -R binbash:binbash /opt/binbash
sudo cp deploy/binbash.service /etc/systemd/system/
sudo systemctl enable --now binbash
```

The unit sets `WorkingDirectory=/opt/binbash`, so `binbash.toml` there (and the `./data` database path) are picked up with no edits to the unit file. Check on it with `systemctl status binbash` and `journalctl -u binbash -f`.

The unit also references an optional `/opt/binbash/.env`. You don't need it when you configure via the TOML file — it's only there if you'd rather set a `BINBASH_*` variable or two (for example, keeping the password out of the config file and passing it as `BINBASH_PASSWORD=...` in that `.env`). The unit starts fine whether or not the file exists.

## Running with Docker

A `Dockerfile` is included as a convenience wrapper around the same binary — it's not the primary way to run binbash, but it's there if you already run everything in containers.

```sh
docker build -t binbash .
docker run -d \
  --name binbash \
  -p 8080:8080 \
  -e BINBASH_PASSWORD=change-this-to-something-strong \
  -v ./data:/data \
  binbash
```

The image sets `BINBASH_DB_PATH=/data/binbash.db`, so mount `/data` to persist the database (and any auto-backups, if you point `auto_backup_dir` somewhere under `/data`). You can pass any setting as a `-e BINBASH_*` flag, or mount your own config file to `/app/binbash.toml`.

## Put a reverse proxy in front

binbash only speaks plain HTTP and is protected by a single shared password. Without HTTPS, that password travels in plaintext — so **don't expose binbash directly to the internet.** Either keep it on a private/VPN-only network, or put a reverse proxy (Caddy, nginx, Traefik) in front to terminate HTTPS. Caddy in particular will get you an automatic certificate in a couple of lines.

## Building from source

You only need this if you want to modify binbash or build for a platform without a prebuilt release. Requires Go 1.25+.

```sh
git clone https://github.com/thinkscotty/binbash.git
cd binbash
go build -o binbash .
```

That produces the same single static binary the releases ship. To run straight from source during development:

```sh
BINBASH_PASSWORD=changeme go run .
```

## License

MIT — see [`LICENSE`](LICENSE).
