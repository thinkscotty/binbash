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
- [Resetting a forgotten password](#resetting-a-forgotten-password)
- [Backups](#backups)
  - [Migrating from Homebox](#migrating-from-homebox)
- [Running as a service (systemd)](#running-as-a-service-systemd)
- [Running with Docker](#running-with-docker)
- [Upgrading](#upgrading)
- [Exposing binbash to the internet](#exposing-binbash-to-the-internet)
  - [1. Terminate HTTPS with a reverse proxy](#1-terminate-https-with-a-reverse-proxy)
  - [2. Bind binbash to localhost](#2-bind-binbash-to-localhost)
  - [3. Use a strong password](#3-use-a-strong-password)
  - [4. Ban repeat offenders with fail2ban (optional)](#4-ban-repeat-offenders-with-fail2ban-optional)
  - [What binbash does for you](#what-binbash-does-for-you)
  - [What binbash does not do](#what-binbash-does-not-do)
- [Building from source](#building-from-source)
- [License](#license)

## What you need

- A hosting computer. This is a web application, not a desktop application. This can run with very small resources (e.g. Raspberry Pi or small VPS), but the computer will need to be continually network-connected. A VPS or Home Server running Linux is recommended.
- Enough knowledge to use a command terminal and set up basic web services
- The application is a self-contained binary, so it has no dependencies. On a home network you can run it as-is. To reach it from outside your home network you'll also need a reverse proxy (Caddy, nginx, Traefik) to put HTTPS in front of it — binbash speaks plain HTTP, so without one your password crosses the internet in the clear. See [Exposing binbash to the internet](#exposing-binbash-to-the-internet).

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

Because this file holds a password (and, later, possibly an API key), keep it to yourself:

```sh
chmod 600 binbash.toml
```

binbash will warn you at startup if you forget.

> **Why a file and not just environment variables?** A config file is easy to keep, back up, and edit later without remembering a string of `export` commands — and it's the recommended way to run binbash. Environment variables still work and still take precedence when set, which is handy for one-off overrides (see [Overriding with environment variables](#overriding-with-environment-variables)).

### 5. Run it

```sh
./binbash
```

You should see:

```
config: loaded ./binbash.toml
binbash <version> listening on :8080
```

The SQLite database is created automatically at `./data/binbash.db` on first run.

### 6. Sign in

Open <http://localhost:8080> (or `http://<server-ip>:8080` from another device on your network) and sign in with the password from your config file.

That password only **bootstraps** your account on first run. Change it any time from **Settings → Change password** in the app — from then on the database is the source of truth, and editing `password` in the file again won't change your login.

Which means that once you've signed in and set a real password in the app, **you can delete the `password` line from `binbash.toml` entirely.** binbash starts fine without it, and you're no longer keeping a plaintext password on disk that doesn't even open anything. It'll remind you of this at startup if you leave it there.

> **Forgot your password?** There's no email reset — binbash has no idea who you are. Instead, prove you own the server by running this on it:
>
> ```sh
> ./binbash -reset-password
> ```
>
> It asks for a new password (twice), sets it, and signs out every device. Then restart binbash so it picks the change up. See [Resetting a forgotten password](#resetting-a-forgotten-password) for the details.

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

# Which network interface to listen on. The default accepts connections from
# anywhere, which is what you want on a home network. If you're putting binbash
# on the public internet behind a reverse proxy, set this to "127.0.0.1" so the
# app can ONLY be reached through the proxy. See "Exposing binbash to the
# internet" below.
bind_address = "0.0.0.0"

# Reverse proxies whose "the real visitor is X" headers binbash should believe.
# Defaults to the local machine, which covers a proxy running on the same box
# — the usual setup, and nothing to change. If your proxy runs elsewhere (a
# Docker container, another server), list its address or network here, or
# binbash will think every visitor is the proxy.
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

### Every setting

| Config file | `[ai]` table? | Default | Purpose |
|---|---|---|---|
| `password` | no | *(required on first run only)* | Login password (8–72 characters) used to **create** your account on first run. Once the account exists this value is ignored — change the password in-app, then delete this line. |
| `port` | no | `8080` | HTTP listen port. |
| `bind_address` | no | `0.0.0.0` | Which interface to listen on. `0.0.0.0` is every interface; `127.0.0.1` restricts binbash to the local machine, so only a reverse proxy on that machine can reach it. Must be a literal IP address. |
| `trusted_proxies` | no | `["127.0.0.1", "::1"]` | Reverse proxies whose `X-Forwarded-For` / `X-Forwarded-Proto` headers binbash trusts, as IPs or CIDR ranges. Anything not listed here has those headers ignored — that's what stops a visitor from simply *claiming* to be someone else. Set to `[]` to trust nobody. |
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
| `BINBASH_BIND_ADDRESS` | `bind_address` |
| `BINBASH_TRUSTED_PROXIES` | `trusted_proxies` (comma-separated, e.g. `172.17.0.0/16,10.0.0.5`) |
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

## Resetting a forgotten password

binbash has no usernames and no email address, so there's no "forgot password" link to click — there'd be nobody to send it to. Instead, the proof that you're the owner is that **you can get to the server**. Run this on the machine binbash runs on:

```sh
./binbash -reset-password
```

It asks for a new password twice, sets it, and signs out every device that was logged in. Then restart binbash — until you do, the running copy still has the old password held in memory:

```sh
sudo systemctl restart binbash
```

Two things to know:

- **Run it as the same user binbash runs as.** On a systemd install that's the `binbash` user, so use `sudo -u binbash ./binbash -reset-password`. If you run it as root by mistake it will stop and tell you, rather than leaving root-owned files behind that binbash itself then can't write.
- It works on a brand-new install too, creating the account — so it's also a way to set the first password without ever putting one in the config file.

To script it (in a container, say), pipe the new password in:

```sh
echo 'a-new-strong-password' | ./binbash -reset-password
```

## Backups

Visit **Backup** in the nav to download a CSV of your whole inventory (one row per item, with its bin's name, category, and description), or to import a CSV back in. Importing adds to what you already have by default; check **Replace existing inventory** to wipe and reload from the file instead. The search page nudges you to back up once you've added 50+ items since the last one.

To automate it, set `auto_backup_dir` to a writable directory. binbash then writes one of these CSVs automatically once that threshold is crossed, keeping the 5 most recent.

### Migrating from Homebox

The same **Import** button also accepts a [Homebox](https://homebox.software) CSV item export — binbash detects the format automatically, so you just upload the file Homebox gives you. It brings across each item's **name**, **description**, **tags** (as keywords), and **location**; Homebox-only fields such as quantity, warranty, and purchase details are dropped, since binbash tracks *where* things are rather than stock levels or provenance.

Because binbash bins are flat while Homebox locations nest, the import looks at each item's location path and uses the **deepest part that names a bin** as the binbash bin — so `Garage / Bin 3` and `Garage / Bin 3 / Small Tray` both land in **Bin 3**. Items whose location names no bin at all (e.g. loose in `Garage`) are skipped rather than dumped into a catch-all, and the result message tells you how many were skipped. Bins are matched to any you already have by name, or created as needed, so it's safe to run into an existing inventory — though a migration is usually cleanest into an empty one.

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

If you put a **containerised reverse proxy** in front of binbash, that proxy isn't on localhost as far as the container is concerned, so tell binbash to trust it — otherwise binbash sees every visitor as the proxy and one person's failed logins lock out everybody:

```sh
-e BINBASH_TRUSTED_PROXIES=172.17.0.0/16   # your Docker network
```

Also note that the in-app updater refuses to run inside a container (there'd be nothing to update — the image is immutable). Pull a new image instead.

## Upgrading

Your inventory lives in a SQLite database file (`data/binbash.db` by default) that is completely separate from the `binbash` binary, and binbash applies any schema changes automatically the next time it starts. So upgrading is just **swapping in the new binary** — you don't export, re-import, or migrate anything by hand, and your data stays exactly where it is.

**Restoring from a CSV is _not_ the upgrade path.** A CSV backup is a safety net for disaster recovery or moving to a new machine; it deliberately doesn't carry AI tags, timestamps, or internal IDs, so round-tripping your whole inventory through one would mark every item as un-tagged again (and a later AI-tag run would re-tag, and re-bill, all of it). Keep the database file in place and none of that is touched.

### Easiest: update from inside the app

Open **Settings → Check for updates**. If a newer release exists, binbash offers an **Update now** button that does the whole swap for you:

1. It reminds you to download a CSV backup (one click, right there), and automatically saves a point-in-time snapshot of your database (to your `auto_backup_dir` if configured, otherwise next to the database; the newest 3 are kept).
2. It downloads the right build for your platform from GitHub, verifies it against the release's `SHA256SUMS.txt`, and test-runs it before touching anything.
3. It swaps the binary in place — the old one is kept alongside as `binbash.old` in case you ever want to roll back — and restarts itself. The page reloads onto the new version a few seconds later, still signed in.

Requirements: the user binbash runs as must own the directory the binary lives in (the [systemd install](#running-as-a-service-systemd) above already sets that up with `chown -R`), and the server needs to reach `github.com`. In-app update isn't available on Windows or inside Docker — use the manual steps below or redeploy the image.

### Manual upgrade: before you start

Take a quick safety copy first, in case you ever want to roll back — schema migrations only move **forward**, so a new binary can upgrade an old database, but an old binary can't open a newer one:

- **Easiest:** open **Backup** in the app and download a CSV.
- **Complete:** with binbash stopped, copy the database and its two sidecar files together — `binbash.db`, `binbash.db-wal`, and `binbash.db-shm`. That's a byte-for-byte snapshot you restore by copying them back.

It's also worth [verifying the new download's checksum](#2-verify-the-download-optional) before you install it.

### systemd install

From your freshly downloaded and extracted release:

```sh
sudo systemctl stop binbash                  # let it flush and release the database
sudo cp binbash /opt/binbash/binbash         # overwrite the old binary
sudo chown binbash:binbash /opt/binbash/binbash
sudo systemctl start binbash                 # applies any new migrations on boot
```

Then confirm it came back up cleanly:

```sh
systemctl status binbash
journalctl -u binbash -n 20
```

You'll see the usual `binbash <version> listening on :8080`. Your config file and `./data` database are untouched — nothing else to change.

### Docker

Rebuild (or pull) the image, then recreate the container. The `-v ./data:/data` mount keeps your database across the swap:

```sh
docker build -t binbash .                     # or pull the new image
docker stop binbash && docker rm binbash
docker run -d \
  --name binbash \
  -p 8080:8080 \
  -e BINBASH_PASSWORD=change-this-to-something-strong \
  -v ./data:/data \
  binbash
```

Because the database lives on the mounted volume, it survives the container being removed and recreated.

### If something looks wrong

Check the logs first (`journalctl -u binbash` or `docker logs binbash`). A migration that fails is logged and stops startup rather than half-applying, so your data isn't left in a partial state. To roll back, reinstall the previous binary — and if a new migration had already run, restore the database snapshot you copied above.

## Exposing binbash to the internet

Running binbash on your home network needs nothing special. Putting it on a public address — so you can reach your inventory from anywhere, or share it with someone who isn't on your wifi — means it will be found and probed by strangers within hours. That's normal and fine, but it changes what you have to get right.

Four steps. The first two are not optional.

### 1. Terminate HTTPS with a reverse proxy

binbash speaks plain HTTP. Without HTTPS in front of it, your password and session cookie cross the internet in the clear for anyone on the path to read. A reverse proxy handles this. Caddy does it in two lines and gets a certificate automatically:

```
binbash.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

nginx and Traefik work equally well; you'll need to point them at a certificate yourself (Certbot is the usual answer).

### 2. Bind binbash to localhost

The proxy is useless if people can just skip it. By default binbash listens on every network interface, which on a public server means `http://your-server-ip:8080` serves your inventory over **unencrypted HTTP**, straight past the proxy you just set up. Close that door:

```toml
bind_address = "127.0.0.1"
```

Now the only way in is through the proxy. Restart binbash, then confirm from another machine that `http://your-server-ip:8080` refuses the connection. A firewall rule blocking port 8080 from outside is a good second layer.

**If your proxy runs in Docker or on a different machine** than binbash, also tell binbash where it is:

```toml
trusted_proxies = ["172.17.0.0/16"]   # your proxy's address or network
```

binbash ignores "the real visitor is X" headers from anyone not on this list — that's what stops a stranger from simply claiming to be someone else to dodge the login lockout. The default covers a proxy on the same machine, which is the common case. Get this wrong and binbash will think every visitor is your proxy, which lumps everyone into one lockout bucket.

### 3. Use a strong password

binbash has one password and no usernames. On the internet, that password is the entire lock on your front door. Use a long random one from a password manager. Change it in **Settings → Change password** — which also signs out every other device, so it doubles as the panic button if you ever think a session leaked.

Then **delete the `password` line from `binbash.toml`**. It was only ever needed to create the account, and once it's gone there's no plaintext password sitting on the server at all. Make sure the config file is `chmod 600` regardless — it may still hold an AI API key.

### 4. Ban repeat offenders with fail2ban (optional)

binbash already locks an IP out for 15 minutes after 5 wrong passwords. fail2ban goes further and drops repeat offenders at the firewall, so their traffic never reaches the app at all. Copy the two files from [`deploy/fail2ban/`](deploy/fail2ban/):

```sh
sudo cp deploy/fail2ban/binbash.conf /etc/fail2ban/filter.d/binbash.conf
sudo cp deploy/fail2ban/jail.local   /etc/fail2ban/jail.d/binbash.local
sudo systemctl restart fail2ban
sudo fail2ban-client status binbash
```

Before enabling it, glance at `journalctl -u binbash | grep "auth failure"` and check the addresses look like real visitors rather than your own proxy — if they're all `127.0.0.1`, fix `trusted_proxies` first, or fail2ban will end up banning your proxy.

### What binbash does for you

Once it's behind HTTPS, binbash handles the rest without configuration:

- **The session cookie is marked `Secure`** when you're on HTTPS, so the browser will never send it over an unencrypted connection.
- **Login attempts are rate-limited per visitor** — 5 tries, then a 15-minute lockout — and every failure is logged with the real client IP.
- **Changing your password signs out every other session**, so a leaked cookie can actually be revoked.
- **Cross-site requests are rejected**, so a malicious page you happen to visit can't quietly make your browser delete a bin or wipe your inventory.
- **Security headers** (`Content-Security-Policy`, `X-Frame-Options`, `nosniff`, `Referrer-Policy`, HSTS) are set on every response — binbash can't be framed for a clickjacking attack, and the page can't load or send anything to a third-party host.
- **Its files are kept private.** The database, its write-ahead log, backups and pre-update snapshots are all created readable only by the user binbash runs as (`0600`, in a `0700` directory), and an install upgraded from an older version gets tightened on the next start. This matters more than it sounds: the database stores the key used to sign session cookies, so anyone who can *read that file* can mint themselves a valid session and walk in without a password at all. On a machine running more than one service, that's the difference between one compromised app and two.

### What binbash does not do

Be clear-eyed about the trade-off you're accepting: **one password, no user accounts, no two-factor.** That's a deliberate design choice for a household inventory app, and with a strong password behind HTTPS it's a reasonable one — but it is the whole security model.

If you want a stronger front door without changing binbash, put an authenticating layer in front of it. A [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/) with Access (free tier, emails a one-time code before anyone reaches binbash, and your server port never faces the internet at all) or [Tailscale](https://tailscale.com/) (private network, no public exposure) both work well and need no changes to binbash.

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
