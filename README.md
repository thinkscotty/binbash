# binbash

Do you keep your stuff in bins or containers and often find yourself opening half a dozen bins in search of a single item? I did. 'binbash' is my attempt to fix that.

'binbash' is a simplified home inventory web app designed for speed and ease of use. It seeks to answer the question, "What container should I look in for a specific item?" The intention is to create a database frontend that's simple enough for anyone to use and minimizes the time required to enter your items into the database.

It makes use of some simple quality-of-life features, such as optional AI-assisted item tagging to help surface synonyms or alternative item names during search.

Binbash is a single, self-contained binary. There's no database server to run, no runtime to install, and no build step required — download one file, create a small config next to it, and start it.

Binbash is fully free and open source.

## Features
- **Bin Creation**: Name containers and give them an optional type and description.
- **Speedy Item Creation**: Other home inventory apps made adding items slower and more complex than I found optimal, so the item-entry process is designed to be fast and minimal. Nothing is required but an item name and a bin assignment — description and tags are optional.
- **Powerful Search**: Item search is designed to surface stems, plurals, and inexact matches (e.g. "Emily's roller blades" will surface "roller blade"). Better matches are surfaced higher in results.
- **Manual Item Tagging**: You can tag items with as many search keywords as you like.
- **AI Item Tagging**: If you have hundreds (or thousands) of items to enter, it's useful to enter as little text as you can for speed and simplicity. Use the AI of your choice to generate additional keyword tags from your item's name and descriptions. The intention is to help you make your items as easy as possible to find without slowing you down. Config settings allow you to set the tag breadth and count you prefer.
- **Simplified Backup and Restore**: Back up your inventory into a small CSV file with a single button press, as often as you want.
- **Single User**: A conscious choice to make the app as simple to use as possible. One shared password allows your whole family to use the application.
- **In-App Updates**: Get security and feature updates with a button press.
- **Widely Compatible**: Built with Go and HTMX (zero JavaScript) and released as precompiled builds for Linux, Windows, and Mac, with AMD64 and ARM64 versions for Linux and Mac. A Dockerfile is also included if Docker is your thing.

## Contents

- [What you need](#what-you-need)
- [Install](#install)
- [Configuration](#configuration)
- [AI tagging](#ai-tagging)
- [Backups](#backups)
- [Running as a service (systemd)](#running-as-a-service-systemd)
- [Upgrading](#upgrading)
- [Exposing binbash to the internet](#exposing-binbash-to-the-internet)
- [License](#license)

Docker, environment variables, building from source, the full settings reference, and the deeper security details all live in [**README_ADVANCED.md**](README_ADVANCED.md).

## What you need

- A hosting computer. This is a web-based application, not a desktop application; you reach your database in a browser. This can run with very light resources (e.g. Raspberry Pi or small VPS), but the computer will need to be continually network-connected. A VPS or Home Server running Linux is recommended.
- Enough knowledge to use a command terminal and set up basic web services
- The application is a self-contained binary, so it has no dependencies. On a home network you can run it as-is. To reach it from outside your home network you'll also need a reverse proxy to put HTTPS in front of it — see [Exposing binbash to the internet](#exposing-binbash-to-the-internet).

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

Every release also ships a `SHA256SUMS.txt` if you'd like to [verify the download](README_ADVANCED.md#verifying-a-download).

### 2. Extract

```sh
tar xzf binbash-$VERSION-linux-amd64.tar.gz
cd binbash-$VERSION-linux-amd64
```

On Windows, unzip the `.zip` and open a terminal in the extracted folder. Each archive contains the `binbash` binary and the license; recent releases also include a fully-commented `binbash.example.toml` and a sample `deploy/binbash.service` you can copy instead of writing them from scratch.

**macOS note:** the binary is unsigned, so the first time you run it Gatekeeper may block it. Clear the download quarantine flag once and you're set:

```sh
xattr -d com.apple.quarantine binbash
```

### 3. Create a config file

binbash reads its settings from a `binbash.toml` file sitting next to the binary. Create one now — at minimum it needs a login password (8 characters or more):

```toml
# binbash.toml
password = "change-this-to-something-strong"
```

That's genuinely all you need to start. Every other setting is optional and covered in [Configuration](#configuration) below.

Because this file holds a password (and, later, possibly an API key), keep it to yourself:

```sh
chmod 600 binbash.toml
```

binbash will warn you at startup if you forget.

### 4. Run it

```sh
./binbash
```

You should see:

```
config: loaded ./binbash.toml
binbash <version> listening on :8080
```

The SQLite database is created automatically at `./data/binbash.db` on first run.

### 5. Sign in

Open <http://localhost:8080> (or `http://<server-ip>:8080` from another device on your network) and sign in with the password from your config file.

That password only **bootstraps** your account on first run. Change it any time from **Settings → Change password** in the app — from then on the database is the source of truth, and editing `password` in the file again won't change your login.

Which means that once you've signed in and set a real password in the app, **you can delete the `password` line from `binbash.toml` entirely.** binbash starts fine without it, and you're no longer keeping a plaintext password on disk that doesn't even open anything. It'll remind you of this at startup if you leave it there.

> **Forgot your password?** There's no email reset — binbash has no idea who you are. Instead, prove you own the server by running `./binbash -reset-password` on it. It asks for a new password (twice), sets it, and signs out every device; restart binbash afterwards so it picks the change up. ([More detail.](README_ADVANCED.md#resetting-a-forgotten-password))

## Configuration

Here's a complete, annotated config file. Every field is optional except `password`, and any field you leave out falls back to the default shown:

```toml
# ---- Core ----

# Login password used to bootstrap your account on first run (min 8 chars).
# Required on first run only; delete it once you've set a real password in-app.
password = "change-this-to-something-strong"

# HTTP port to listen on.
port = "8080"

# Which network interface to listen on. The default accepts connections from
# anywhere, which is what you want on a home network. If you're putting binbash
# on the public internet behind a reverse proxy, set this to "127.0.0.1" so the
# app can ONLY be reached through the proxy. See "Exposing binbash to the
# internet" below.
bind_address = "0.0.0.0"

# Reverse proxies whose "the real visitor is X" headers binbash should believe.
# The default covers a proxy running on the same box — the usual setup, and
# nothing to change. If your proxy runs elsewhere (a Docker container, another
# server), list its address or network here, or binbash will think every
# visitor is the proxy.
trusted_proxies = ["127.0.0.1", "::1"]

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

By default binbash looks for `binbash.toml` in the directory you run it from. To keep it somewhere else, point `BINBASH_CONFIG` at it:

```sh
BINBASH_CONFIG=/etc/binbash/config.toml ./binbash
```

A config file is the recommended way to run binbash — it's easy to keep, back up, and edit later. Every setting can also be supplied as a `BINBASH_*` environment variable if you'd rather do that (handy in containers); see [the full settings reference](README_ADVANCED.md#every-setting).

## AI tagging

AI tagging is optional and off until you set `[ai].base_url` (plus `api_key` and `model` as your provider requires). Once configured, the **Items** page shows a "Tag up to 25 items" button. It asks the AI for search-friendly keyword suggestions — synonyms, alternate names, categories — for items that haven't been AI-tagged yet, and **appends** them to each item's existing keywords. It never edits or removes what's already there.

Once an item is successfully tagged it's skipped by future runs. An item whose response came back with no usable tags is left untagged so a later run retries it, rather than being marked done with nothing. Click the button again to work through a larger backlog in batches of 25.

Any OpenAI-compatible endpoint works, including reasoning models (Qwen3, DeepSeek-R1, and the like). Those spend part of their reply "thinking" before answering, so binbash requests a generous token budget to let them finish and still return usable tags.

If the suggestions aren't quite to your taste — you want British *and* American spellings, or single words only, or no broad categories — you can tell the AI so in your own words with `tag_prompt`. See [Customizing the tagging prompt](README_ADVANCED.md#customizing-the-tagging-prompt).

## Backups

Visit **Backup** in the nav to download a CSV of your whole inventory (one row per item, with its bin's name, category, and description), or to import a CSV back in. Importing adds to what you already have by default; check **Replace existing inventory** to wipe and reload from the file instead. The search page nudges you to back up once you've added 50+ items since the last one.

To automate it, set `auto_backup_dir` to a writable directory. binbash then writes one of these CSVs automatically once that threshold is crossed, keeping the 5 most recent.

The same **Import** button also accepts a [Homebox](https://homebox.software) CSV export — see [Migrating from Homebox](README_ADVANCED.md#migrating-from-homebox).

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

## Upgrading

Your inventory lives in a SQLite database file (`data/binbash.db` by default) that is completely separate from the `binbash` binary, and binbash applies any schema changes automatically the next time it starts. So upgrading is just **swapping in the new binary** — you don't export, re-import, or migrate anything by hand, and your data stays exactly where it is.

### Easiest: update from inside the app

Open **Settings → Check for updates**. If a newer release exists, binbash offers an **Update now** button that does the whole swap for you:

1. It reminds you to download a CSV backup (one click, right there), and automatically saves a point-in-time snapshot of your database (to your `auto_backup_dir` if configured, otherwise next to the database; the newest 3 are kept).
2. It downloads the right build for your platform from GitHub, verifies it against the release's `SHA256SUMS.txt`, and test-runs it before touching anything.
3. It swaps the binary in place — the old one is kept alongside as `binbash.old` in case you ever want to roll back — and restarts itself. The page reloads onto the new version a few seconds later, still signed in.

Requirements: the user binbash runs as must own the directory the binary lives in (the [systemd install](#running-as-a-service-systemd) above already sets that up with `chown -R`), and the server needs to reach `github.com`. In-app update isn't available on Windows or inside Docker.

### Manual upgrade

Take a safety copy first: schema migrations only move **forward**, so a new binary can upgrade an old database, but an old binary can't open a newer one. Download a CSV from the **Backup** page, or — with binbash stopped — copy `binbash.db` along with its `binbash.db-wal` and `binbash.db-shm` sidecars for a byte-for-byte snapshot.

Then, from your freshly downloaded and extracted release:

```sh
sudo systemctl stop binbash                  # let it flush and release the database
sudo cp binbash /opt/binbash/binbash         # overwrite the old binary
sudo chown binbash:binbash /opt/binbash/binbash
sudo systemctl start binbash                 # applies any new migrations on boot
```

Confirm it came back up cleanly with `systemctl status binbash` and `journalctl -u binbash -n 20`; you'll see the usual `binbash <version> listening on :8080`. Your config file and `./data` database are untouched.

If a migration fails it's logged and stops startup rather than half-applying, so your data isn't left in a partial state. To roll back, reinstall the previous binary — and if a new migration had already run, restore the database snapshot you copied above.

> **Restoring from a CSV is _not_ the upgrade path.** A CSV backup is a safety net for disaster recovery or moving to a new machine; it deliberately doesn't carry AI tags, timestamps, or internal IDs, so round-tripping your whole inventory through one would mark every item as un-tagged again (and a later AI-tag run would re-tag, and re-bill, all of it). Keep the database file in place and none of that is touched.

## Exposing binbash to the internet

Running binbash on your home network needs nothing special. Putting it on a public address — so you can reach your inventory from anywhere — means it will be found and probed by strangers within hours. That's normal and fine, but two things are then **not optional**:

**1. Put HTTPS in front of it with a reverse proxy.** binbash speaks plain HTTP; without HTTPS, your password and session cookie cross the internet in the clear for anyone on the path to read. Caddy does it in two lines and gets a certificate automatically:

```
binbash.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

nginx and Traefik work equally well.

**2. Bind binbash to localhost**, so nobody can simply skip the proxy and reach `http://your-server-ip:8080` over unencrypted HTTP:

```toml
bind_address = "127.0.0.1"
```

Restart binbash, then confirm from another machine that `http://your-server-ip:8080` refuses the connection.

Beyond that: use a long random password (it's the entire lock on your front door — binbash has one password, no usernames, and no two-factor), and delete the `password` line from `binbash.toml` once you've set a real one in the app.

Once it's behind HTTPS, binbash handles the rest without configuration — `Secure` session cookies, login rate-limiting with a 15-minute lockout, password changes that sign out every other device, cross-site request rejection, security headers, and private file permissions on the database and backups.

[**README_ADVANCED.md**](README_ADVANCED.md#hardening-a-public-install) covers the rest: proxies running in Docker or on another machine (`trusted_proxies`), fail2ban, what each built-in protection actually does, and stronger front doors like Cloudflare Access or Tailscale.

## License

MIT — see [`LICENSE`](LICENSE).
