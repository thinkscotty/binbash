# binbash

Do you keep a lot of stuff in bins or containers and often find yourself opening half a dozen bins in search of a single item? So do I. 'binbash' is my attempt to solve that problem.

'binbash' is a stripped-down home inventory web app designed for speed and simplicity. It seeks to answer the question, "What container is my item in?" as fast and simply as possible. The intention is to create a database frontend that's simple enough for anyone to use and minimizes the time required to enter your items into the database.

'binbash' adds some simple quality of life features designed to speed up item-entry. These include intelligent automatic title case-ing of item names,  and optional AI-assisted item tagging to help surface synonyms or alternative item names during search. The UI is also designed with mobile OS browsers as the primary usage target as that's what most people will be using while organizing or searching for items.

'binbash' is a single, self-contained binary. There's no database server to run, no runtime to install, and no build step required — download one file, create a small config next to it, and start it.

'binbash' is fully free and open source.

## Features
- **Bin Creation**: Name containers and give them an optional type and description.
- **Speedy Item Creation**: Other home inventory apps made adding items slower and more complex than I found optimal, so the item-entry process is designed to be fast and minimal. Nothing is required but an item name and a bin assignment — description and tags are optional.
- **Powerful Search**: Item search is designed to surface stems, plurals, and inexact matches (e.g. "Emily's roller blades" will surface "roller blade"). Better matches are surfaced higher in results.
- **Manual Item Tagging**: You can tag items with as many search keywords as you like.
- **AI Item Tagging**: If you have hundreds (or thousands) of items to enter, it's useful to enter as little text as you can for speed and simplicity. Use the AI of your choice to generate additional keyword tags from your item's name and descriptions. The intention is to help you make your items as easy as possible to find without slowing you down. Config settings allow you to set the tag breadth and count you prefer.
- **Optional Color Tags**: Turn on a row of color swatches in the item form and tag an item with one tap — handy if you keep small things in colored zippered bags inside a bin. A matching dot then appears next to the item everywhere it's listed, so you can see at a glance which bag to reach for. Off by default, and the field's name is yours to set if you'd rather code something else by color.
- **Simplified Backup and Restore**: Back up your inventory into a small CSV file with a single button press, as often as you want.
- **Single User**: A conscious choice to make the app as simple to use as possible. One shared password allows your whole family to use the application.
- **In-App Updates**: Get security and feature updates with a button press.
- **Widely Compatible**: Built with Go and HTMX (zero JavaScript) and released as precompiled builds for Linux, Windows, and Mac, with AMD64 and ARM64 versions for Linux and Mac. A Dockerfile and a Compose file are included if Docker is your thing.

## Contents

**The basics** — everything a normal install needs:

- [What you need](#what-you-need)
- [Install](#install)
- [Configuration](#configuration)
- [AI tagging](#ai-tagging)
- [Color tags](#color-tags)
- [Backups](#backups)
- [Running as a service (systemd)](#running-as-a-service-systemd)
- [Upgrading](#upgrading)
- [Exposing binbash to the internet](#exposing-binbash-to-the-internet)
- [Installing with Docker](#installing-with-docker)

**[Advanced](#advanced)** — come back for these when you want them:

- [Verifying a download](#verifying-a-download)
- [Every setting](#every-setting)
- [Environment variables](#environment-variables)
- [Customizing the tagging prompt](#customizing-the-tagging-prompt)
- [More about Docker](#more-about-docker)
- [Resetting a forgotten password](#resetting-a-forgotten-password)
- [Migrating from Homebox](#migrating-from-homebox)
- [Hardening a public install](#hardening-a-public-install)
- [Building from source](#building-from-source)
- [License](#license)

## What you need

- A hosting computer. This is a web-based application, not a desktop application; you reach your database in a browser. This can run with very light resources (e.g. Raspberry Pi or small VPS), but the computer will need to be continually network-connected. A VPS or Home Server running Linux is recommended.
- Enough knowledge to use a command terminal and set up basic web services
- The application is a self-contained binary, so it has no dependencies. On a home network you can run it as-is. To reach it from outside your home network you'll also need a reverse proxy to put HTTPS in front of it — see [Exposing binbash to the internet](#exposing-binbash-to-the-internet).

## Install

Prefer containers? Skip to [Installing with Docker](#installing-with-docker) — it's a complete, self-contained path that doesn't need anything from this section.

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

Every release also ships a `SHA256SUMS.txt` if you'd like to [verify the download](#verifying-a-download).

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

> **Forgot your password?** There's no email reset — binbash has no idea who you are. Instead, prove you own the server by running `./binbash -reset-password` on it. It asks for a new password (twice), sets it, and signs out every device; restart binbash afterwards so it picks the change up. ([More detail.](#resetting-a-forgotten-password))

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

# Optional: extra instructions in your own words, appended to and given priority
# over the built-in prompt (triple-quoted so it can span several lines). Omit it
# and the built-in prompt is used as-is. See "AI tagging" below.
# tag_prompt = """
# Use British spellings, and add the American spelling as a separate tag.
# Every tag must be a single word.
# """

# ---- Color tags (optional) ----
# Adds a row of color swatches to the item form. Off unless enabled is true.
# See "Color tags" below.
[color_tags]
enabled = false
label   = "Color"   # what the field is called on the item form, e.g. "Bag"
```

By default binbash looks for `binbash.toml` in the directory you run it from. To keep it somewhere else, point `BINBASH_CONFIG` at it:

```sh
BINBASH_CONFIG=/etc/binbash/config.toml ./binbash
```

A config file is the recommended way to run binbash — it's easy to keep, back up, and edit later. Every setting can also be supplied as a `BINBASH_*` environment variable if you'd rather do that (handy in containers); see [the full settings reference](#every-setting).

## AI tagging

AI tagging is optional and off until you set `[ai].base_url` (plus `api_key` and `model` as your provider requires). Once configured, the **Items** page shows a "Tag up to 25 items" button. It asks the AI for search-friendly keyword suggestions — synonyms, alternate names, categories — for items that haven't been AI-tagged yet, and **appends** them to each item's existing keywords. It never edits or removes what's already there.

Once an item is successfully tagged it's skipped by future runs. An item whose response came back with no usable tags is left untagged so a later run retries it, rather than being marked done with nothing. Click the button again to work through a larger backlog in batches of 25.

Any OpenAI-compatible endpoint works, including reasoning models (Qwen3, DeepSeek-R1, and the like). Those spend part of their reply "thinking" before answering, so binbash requests a generous token budget to let them finish and still return usable tags.

If the suggestions aren't quite to your taste — you want British *and* American spellings, or single words only, or no broad categories — you can tell the AI so in your own words with `tag_prompt`. See [Customizing the tagging prompt](#customizing-the-tagging-prompt).

## Color tags

Small things often live in colored zippered bags inside a bin. Color tags let you record that with one tap instead of typing "in the red bag" into a description.

Turn them on with:

```toml
[color_tags]
enabled = true
label   = "Bag"
```

The item form then shows a row of swatches — white, black, red, orange, yellow, green, light blue, dark blue, purple, and pink, plus **None**. Pick one and a matching dot appears next to that item in search results, on the Items page, and inside its bin. Pick None and no dot shows, which tells you just as much: the item is loose in the bin rather than bagged.

Nothing about it is bag-specific, which is why `label` is yours to set. Use the colors for who owns something, which room it belongs to, or anything else worth coding by color.

A few details worth knowing:

- **The picker resets to None for every new item.** It deliberately doesn't remember your last choice the way the bin dropdown does — an unnoticed leftover color would claim an item is in a bag it isn't, and a wrong color sends you to the wrong bag.
- **Colors aren't searchable.** They're for scanning a list at a glance, not for querying.
- **Turning the feature off is safe.** The swatches and dots disappear, but your colors stay in the database and in every CSV backup, and come straight back if you switch it on again.

## Backups

Visit **Backup** in the nav to download a CSV of your whole inventory (one row per item, with its bin's name, category, and description), or to import a CSV back in. Importing adds to what you already have by default; check **Replace existing inventory** to wipe and reload from the file instead. The search page nudges you to back up once you've added 50+ items since the last one.

To automate it, set `auto_backup_dir` to a writable directory. binbash then writes one of these CSVs automatically once that threshold is crossed, keeping the 5 most recent.

The same **Import** button also accepts a [Homebox](https://homebox.software) CSV export — see [Migrating from Homebox](#migrating-from-homebox).

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

(Running under Docker? Upgrading is a rebuild instead — see [Upgrading the container](#upgrading-the-container).)

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

(Under Docker this second step works differently — the container must bind `0.0.0.0` internally, and it's the `127.0.0.1:` prefix on the published port that limits exposure. The shipped `compose.yaml` already does this.)

Beyond that: use a long random password (it's the entire lock on your front door — binbash has one password, no usernames, and no two-factor), and delete the `password` line from `binbash.toml` once you've set a real one in the app.

Once it's behind HTTPS, binbash handles the rest without configuration — `Secure` session cookies, login rate-limiting with a 15-minute lockout, password changes that sign out every other device, cross-site request rejection, security headers, and private file permissions on the database and backups.

[Hardening a public install](#hardening-a-public-install) covers the rest: proxies running in Docker or on another machine (`trusted_proxies`), fail2ban, what each built-in protection actually does, and stronger front doors like Cloudflare Access or Tailscale.

## Installing with Docker

Docker is a first-class way to run binbash, and the sensible one if your server already hosts several applications. The image is built from the same source as the release binaries and behaves identically — same database file, same settings, same backups. This section is complete on its own: you don't need anything from [Install](#install) above.

There's no image on Docker Hub to pull. You clone the repository and build locally, which takes a couple of minutes the first time and means the image you run is built from source you can read.

The image is [distroless](https://github.com/GoogleContainerTools/distroless): it contains binbash, CA certificates and timezone data, and nothing else. There is no shell, no package manager and no libc inside it. That's why `docker exec ... sh` won't work — and also why there's very little in there to attack.

### Before you start

You need three things on the host:

- **Docker Engine 23 or newer**, with the Compose plugin. Check both at once:

  ```sh
  docker --version && docker compose version
  ```

  If `docker compose version` errors, you have the old standalone `docker-compose`; install the [Compose plugin](https://docs.docker.com/compose/install/linux/) — the commands below all use the modern `docker compose` spelling.

- **git**, to fetch the source.

- **Permission to run Docker.** Either use `sudo` on every command below, or add yourself to the `docker` group (`sudo usermod -aG docker $USER`, then log out and back in).

### 1. Get the source

```sh
git clone https://github.com/thinkscotty/binbash.git
cd binbash
```

Everything from here happens in that directory. Your inventory will live in `./data` inside it, so put it somewhere you're happy to leave it — `/opt/binbash` or `~/binbash` are both fine.

### 2. Create your settings file

Under Docker, settings come from environment variables rather than `binbash.toml`. The ones you'll actually want to change live in a `.env` file:

```sh
cp .env.example .env
chmod 600 .env      # it holds your password
```

Now open `.env` and set these for your machine:

| Variable | What to put | Why it matters |
|---|---|---|
| `PUID` | Your user id — run `id -u` | The container runs as this user. If it doesn't match whoever owns `./data`, binbash refuses to start. |
| `PGID` | Your group id — run `id -g` | Same, for the group. |
| `TZ` | e.g. `America/New_York` | A container has no idea where it is. Without this, every timestamp and log line reads as UTC. |
| `BINBASH_PORT` | `8080` unless that's taken | The port on the host. Published on `127.0.0.1` only — see step 5. |
| `BINBASH_PASSWORD` | 8+ characters, something strong | Creates your account on the very first start. |
| `BINBASH_VERSION` | e.g. `v0.1.0`, or leave `dev` | Stamped into the binary and shown on the settings page, so you can tell which build is running. |

`BINBASH_PASSWORD` is read **only when no account exists yet**. Once you've logged in and set a real password in the app, that line does nothing — delete it (see step 6).

### 3. Create the data directory

```sh
mkdir -p data
```

Do this before starting the container. If Docker has to create the mount point itself it creates it owned by `root`, and a container running as you then can't write to it.

### 4. Build and start

```sh
docker compose up -d --build
```

The first build compiles binbash from source and takes a few minutes; later ones are much faster thanks to layer caching. `-d` runs it in the background, and `restart: unless-stopped` in `compose.yaml` means it comes back automatically after a reboot or a crash.

### 5. Check that it started

```sh
docker compose logs -f binbash
```

You're looking for:

```
binbash <version> listening on :8080
```

Press <kbd>Ctrl</kbd>+<kbd>C</kbd> to stop following the log; the container keeps running. `docker compose ps` shows its state at a glance.

### 6. Sign in

Open <http://localhost:8080> on the host and sign in with the password you put in `.env`.

**Can't reach it from another device?** That's deliberate. `compose.yaml` publishes the port as `127.0.0.1:8080:8080`, meaning the host itself can reach binbash and nothing else on the network can. You have two options:

- **Put a reverse proxy in front** (the right answer if anyone outside your house will use it, and required if it's on a public address). Point it at `127.0.0.1:8080` and see [Exposing binbash to the internet](#exposing-binbash-to-the-internet). If the proxy is itself a container, also read [Putting a reverse proxy in front](#putting-a-reverse-proxy-in-front).
- **Serve your LAN directly** by dropping the `127.0.0.1:` prefix in `compose.yaml`, then `docker compose up -d`. Fine on a trusted home network; it's plain HTTP, so don't do it on a machine facing the internet.

Once you're in, change the password under **Settings → Change password**, then remove the now-meaningless `BINBASH_PASSWORD` line from `.env` and apply it:

```sh
docker compose up -d
```

That last command is how *every* `.env` or `compose.yaml` change takes effect — Compose notices the difference and recreates the container. It doesn't rebuild the image (that's `--build`) and it doesn't touch your data.

### Where your data lives

Everything is in `./data` on the host, mounted into the container at `/data`:

- `data/binbash.db` — your entire inventory (plus its `-wal` and `-shm` sidecars while it's running)
- `data/backups/` — automatic CSV backups, because `compose.yaml` sets `BINBASH_AUTO_BACKUP_DIR=/data/backups`

**Back up that one directory and you have everything.** The container itself is disposable. For a byte-for-byte copy of the database, stop the container first (`docker compose stop`) so it can checkpoint and close cleanly; you can also grab a CSV any time from the **Backup** page in the app with binbash running.

### Changing other settings

Anything in [Every setting](#every-setting) works here — add it to the `environment:` block in `compose.yaml` as its `BINBASH_*` variable. AI tagging, for example:

```yaml
environment:
  BINBASH_AI_BASE_URL: https://api.openai.com/v1
  BINBASH_AI_API_KEY: ${BINBASH_AI_API_KEY:-}
  BINBASH_AI_MODEL: gpt-4o-mini
```

The `${...}` form keeps the actual key in `.env` rather than in a file you might commit. `compose.yaml` ships with those three lines already there, commented out.

If you'd rather use a `binbash.toml` — say you're moving an existing install in and want to keep its config — uncomment the mount line in `compose.yaml`:

```yaml
volumes:
  - ./binbash.toml:/app/binbash.toml:ro
```

Then run `docker compose up -d`. Remember that environment variables win over the file, so don't set the same thing in both places and expect the file to be used.

### Upgrading the container

The in-app updater deliberately refuses to run in a container — the image is immutable, so there'd be nothing to update. Build a new image instead:

```sh
git pull
docker compose up -d --build
```

Your database is in the bind mount, so it survives the container being replaced, and any new schema migrations run automatically at startup. Set `BINBASH_VERSION` in `.env` to the tag you built from so the settings page reports it accurately.

Migrations only move forward — a new binary can open an old database, but not the other way around — so take a CSV backup from the **Backup** page (or copy `./data`) before a big jump. If something looks wrong afterwards, check `docker compose logs binbash`; a failed migration is logged and stops startup rather than half-applying.

### Everyday commands

```sh
docker compose ps                    # is it running?
docker compose logs -f binbash       # follow the log
docker compose restart binbash       # restart it
docker compose stop                  # stop, keeping the container
docker compose up -d                 # start again / apply .env changes
docker compose down                  # stop and remove the container (data stays in ./data)
```

### When something goes wrong

**`binbash cannot use /data: it belongs to the user "root", but binbash is running as "nonroot".`**
`PUID`/`PGID` don't match the owner of `./data`. binbash refuses to start against files it couldn't read back later, so this is a clean error at boot rather than a corrupted database three weeks on. Check the real owner with `ls -ldn data` — the third and fourth columns are the uid and gid — then either fix the values in `.env` or fix the directory: `sudo chown -R "$(id -u):$(id -g)" data`. Apply with `docker compose up -d`.

**`Bind for 127.0.0.1:8080 failed: port is already allocated`**
Something else on the host has that port. Set a different `BINBASH_PORT` in `.env` and run `docker compose up -d`. Only the host side changes; binbash still listens on 8080 inside the container.

**`docker compose exec binbash sh` fails with "executable file not found"**
Expected — the distroless image has no shell. Use `docker compose logs` to see what it's doing, and `docker compose run --rm binbash <flag>` to run binbash itself with a flag.

**Forgotten password.** Run the reset as a throwaway container against the same data:

```sh
docker compose run --rm binbash -reset-password
docker compose restart binbash
```

More on that in [Resetting a forgotten password](#resetting-a-forgotten-password).

**Timestamps look wrong.** `TZ` isn't set, or isn't a valid [tz database name](https://en.wikipedia.org/wiki/List_of_tz_database_time_zones). `timedatectl` shows the host's.

### Moving an existing install into Docker

Already running the binary and want to switch? Your database is a plain file and the container reads exactly the same format, so it's a copy:

```sh
sudo systemctl stop binbash          # or just quit the running binary
cp -a /opt/binbash/data ./data       # -a preserves ownership and modes
sudo chown -R "$(id -u):$(id -g)" ./data
docker compose up -d --build
```

Leave `BINBASH_PASSWORD` out of `.env` — your account and its hashed password are already in the database, and binbash only reads that variable when there's no account yet.

---

# Advanced

Everything below is for people who want it: the full settings reference, environment variables, a public-facing install, prompt customization, and building from source. Nothing here is needed for a normal install on a home network.

## Verifying a download

Each release includes a `SHA256SUMS.txt`. If you'd like to confirm your download wasn't corrupted or tampered with, grab it and check:

```sh
curl -LO "https://github.com/thinkscotty/binbash/releases/download/$VERSION/SHA256SUMS.txt"

# Linux:
sha256sum -c SHA256SUMS.txt --ignore-missing

# macOS:
shasum -a 256 -c SHA256SUMS.txt --ignore-missing
```

You want to see `OK` next to the file you downloaded. It's worth doing this before installing an upgrade, too.

## Every setting

| Config file | Section | Default | Purpose |
|---|---|---|---|
| `password` | — | *(required on first run only)* | Login password (8–72 characters) used to **create** your account on first run. Once the account exists this value is ignored — change the password in-app, then delete this line. |
| `port` | — | `8080` | HTTP listen port. |
| `bind_address` | — | `0.0.0.0` | Which interface to listen on. `0.0.0.0` is every interface; `127.0.0.1` restricts binbash to the local machine, so only a reverse proxy on that machine can reach it. Must be a literal IP address. |
| `trusted_proxies` | — | `["127.0.0.1", "::1"]` | Reverse proxies whose `X-Forwarded-For` / `X-Forwarded-Proto` headers binbash trusts, as IPs or CIDR ranges. Anything not listed here has those headers ignored — that's what stops a visitor from simply *claiming* to be someone else. Set to `[]` to trust nobody. |
| `db_path` | — | `./data/binbash.db` | SQLite database file location. Its parent directory is created automatically. |
| `auto_backup_dir` | — | *(empty = disabled)* | Directory to write automatic CSV backups to. |
| `base_url` | `[ai]` | *(empty = AI off)* | OpenAI-compatible endpoint. Include any path prefix the provider needs (e.g. `https://api.openai.com/v1`) — `/chat/completions` is appended to it. |
| `api_key` | `[ai]` | *(empty)* | API key for the AI endpoint. |
| `model` | `[ai]` | *(empty)* | Model name to request. |
| `tag_count` | `[ai]` | `3` | Max AI-suggested tags per item (`0`–`8`; `0` keeps the feature on but generates no tags). |
| `tag_breadth` | `[ai]` | `moderate` | How closely related suggested tags should be: `narrow`, `moderate`, or `broad`. |
| `tag_prompt` | `[ai]` | *(empty)* | Extra instructions for the AI, in your own words, added to the end of the built-in prompt. See [Customizing the tagging prompt](#customizing-the-tagging-prompt). |
| `enabled` | `[color_tags]` | `false` | Turns the optional per-item color tags on: a row of swatches on the item form, and a matching dot beside the item wherever it's listed. See [Color tags](#color-tags). |
| `label` | `[color_tags]` | `Color` | What the color field is called on the item form (max 30 characters) — `Bag`, `Owner`, `Room`, whatever you're coding by color. |

## Environment variables

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
| `BINBASH_AI_TAG_PROMPT` | `[ai].tag_prompt` |
| `BINBASH_COLOR_TAGS` | `[color_tags].enabled` (`true` or `false`) |
| `BINBASH_COLOR_TAGS_LABEL` | `[color_tags].label` |

You can run binbash with **no config file at all** and configure it entirely through these variables if you prefer — the file is the recommended path, not a required one.

The sample systemd unit at [`deploy/binbash.service`](deploy/binbash.service) references an optional `/opt/binbash/.env` for exactly this. You don't need it when you configure via the TOML file — it's there if you'd rather set a `BINBASH_*` variable or two (for example, keeping the password out of the config file and passing it as `BINBASH_PASSWORD=...`). The unit starts fine whether or not the file exists.

## Customizing the tagging prompt

binbash ships with a prompt that asks for search-friendly keywords and tells the AI *not* to simply echo words already in the item's name. `tag_count` and `tag_breadth` tune it, but if you want something they don't cover, `tag_prompt` lets you say so in plain language:

```toml
[ai]
base_url = "https://api.openai.com/v1"
api_key  = "sk-..."
model    = "gpt-4o-mini"

tag_prompt = """
Use British spellings, and add the American spelling as a separate tag.
Do not suggest broad categories -- only specific names for the thing itself.
Every tag must be a single word.
"""
```

TOML's triple quotes (`"""`) let the value run over several lines. One instruction per line reads well to a model and to you.

Your instructions are **added to the end** of the built-in prompt and declared to take priority over it, so you can contradict the defaults, not just add to them — "no broad categories" really does override the built-in guidance to suggest a category. `tag_count` and `tag_breadth` keep working alongside it.

The one thing you can't override is the response format: binbash needs a plain comma-separated list back, so that instruction always applies. (Without that guarantee, a prompt that talked the model into writing sentences wouldn't give you differently-styled tags, it would give you tags like `Sure` and `here are three keywords`.)

Some things worth knowing:

- **Changes need a restart.** The prompt is read from the config at startup, like every other setting.
- **Only newly-tagged items are affected.** Items already marked AI-tagged are skipped by future runs, so a new prompt applies to the backlog and to items you add from now on — it doesn't retroactively re-tag anything.
- **Try it on a few items first.** Tag one batch, look at what you got, and adjust. Since tags are only ever *appended* to an item's keywords, a prompt that produces junk means editing those items to clean them up rather than a single undo.

## More about Docker

[Installing with Docker](#installing-with-docker) covers a working install. These are the details behind it.

### Putting a reverse proxy in front

A proxy in another container does not reach binbash over loopback, so binbash sees the proxy's address as every visitor's address. Left alone, that means one person's failed logins throttle the whole household, and fail2ban has nothing useful to ban. Tell binbash where the proxy is:

```yaml
environment:
  BINBASH_TRUSTED_PROXIES: 172.18.0.0/16   # your proxy's Docker network
```

Find the network with `docker network inspect <name> -f '{{range .IPAM.Config}}{{.Subnet}}{{end}}'`.

Note that the advice in [Exposing binbash to the internet](#exposing-binbash-to-the-internet) about setting `bind_address = 127.0.0.1` on a public server does **not** apply inside a container. There, binbash must bind `0.0.0.0` to be reachable at all; what limits exposure is the `127.0.0.1:` prefix on the published port in `compose.yaml`.

### Why the container is locked down

`compose.yaml` ships with `read_only: true`, `cap_drop: [ALL]` and `no-new-privileges`. binbash writes to `/data` and nowhere else, so none of that costs anything, and all three are verified working. `stop_grace_period: 30s` is not decoration either: on shutdown binbash drains in-flight requests for up to ten seconds and only then closes SQLite, which is what checkpoints the write-ahead log. Docker's default grace period is also ten seconds, which puts the `SIGKILL` right on top of that close.

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

To script it, pipe the new password in:

```sh
echo 'a-new-strong-password' | ./binbash -reset-password
```

**Under Docker**, run it as a one-off container against the same volume. `--rm`
throws the container away afterwards; the password change is in the database:

```sh
docker compose run --rm binbash -reset-password        # prompts twice
echo 'a-new-strong-password' | docker compose run --rm -T binbash -reset-password
docker compose restart binbash
```

The command's closing note says to restart with `systemctl` — in Docker that is
`docker compose restart binbash`, as above. The point is the same either way: a
running copy holds the old password in memory until it is restarted.

## Migrating from Homebox

The **Import** button on the Backup page also accepts a [Homebox](https://homebox.software) CSV item export — binbash detects the format automatically, so you just upload the file Homebox gives you. It brings across each item's **name**, **description**, **tags** (as keywords), and **location**; Homebox-only fields such as quantity, warranty, and purchase details are dropped, since binbash tracks *where* things are rather than stock levels or provenance.

Because binbash bins are flat while Homebox locations nest, the import looks at each item's location path and uses the **deepest part that names a bin** as the binbash bin — so `Garage / Bin 3` and `Garage / Bin 3 / Small Tray` both land in **Bin 3**. Items whose location names no bin at all (e.g. loose in `Garage`) are skipped rather than dumped into a catch-all, and the result message tells you how many were skipped. Bins are matched to any you already have by name, or created as needed, so it's safe to run into an existing inventory — though a migration is usually cleanest into an empty one.

## Hardening a public install

[Exposing binbash to the internet](#exposing-binbash-to-the-internet) covers the two non-optional steps: terminate HTTPS with a reverse proxy, and set `bind_address = "127.0.0.1"` so nobody can bypass it. This section is the rest.

### Telling binbash where your proxy is

**If your proxy runs in Docker or on a different machine** than binbash, tell binbash where it is:

```toml
trusted_proxies = ["172.17.0.0/16"]   # your proxy's address or network
```

binbash ignores "the real visitor is X" headers from anyone not on this list — that's what stops a stranger from simply claiming to be someone else to dodge the login lockout. The default (`["127.0.0.1", "::1"]`) covers a proxy on the same machine, which is the common case. Get this wrong and binbash will think every visitor is your proxy, which lumps everyone into one lockout bucket.

A firewall rule blocking port 8080 from outside is a good second layer behind `bind_address`.

### Ban repeat offenders with fail2ban

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
