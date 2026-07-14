# binbash — advanced guide

Everything that most people running binbash on a home network will never need. Start with the [README](README.md); come here when you want Docker, environment variables, the full settings reference, a public-facing install, or a build from source.

## Contents

- [Verifying a download](#verifying-a-download)
- [Every setting](#every-setting)
- [Environment variables](#environment-variables)
- [Running with Docker](#running-with-docker)
- [Resetting a forgotten password](#resetting-a-forgotten-password)
- [Migrating from Homebox](#migrating-from-homebox)
- [Hardening a public install](#hardening-a-public-install)
- [Building from source](#building-from-source)

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

You can run binbash with **no config file at all** and configure it entirely through these variables if you prefer — the file is the recommended path, not a required one.

The sample systemd unit at [`deploy/binbash.service`](deploy/binbash.service) references an optional `/opt/binbash/.env` for exactly this. You don't need it when you configure via the TOML file — it's there if you'd rather set a `BINBASH_*` variable or two (for example, keeping the password out of the config file and passing it as `BINBASH_PASSWORD=...`). The unit starts fine whether or not the file exists.

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

Note that the in-app updater refuses to run inside a container (there'd be nothing to update — the image is immutable). Pull a new image instead.

### Upgrading a container

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

Because the database lives on the mounted volume, it survives the container being removed and recreated. If something looks wrong afterwards, check `docker logs binbash` — a failed migration is logged and stops startup rather than half-applying.

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

## Migrating from Homebox

The **Import** button on the Backup page also accepts a [Homebox](https://homebox.software) CSV item export — binbash detects the format automatically, so you just upload the file Homebox gives you. It brings across each item's **name**, **description**, **tags** (as keywords), and **location**; Homebox-only fields such as quantity, warranty, and purchase details are dropped, since binbash tracks *where* things are rather than stock levels or provenance.

Because binbash bins are flat while Homebox locations nest, the import looks at each item's location path and uses the **deepest part that names a bin** as the binbash bin — so `Garage / Bin 3` and `Garage / Bin 3 / Small Tray` both land in **Bin 3**. Items whose location names no bin at all (e.g. loose in `Garage`) are skipped rather than dumped into a catch-all, and the result message tells you how many were skipped. Bins are matched to any you already have by name, or created as needed, so it's safe to run into an existing inventory — though a migration is usually cleanest into an empty one.

## Hardening a public install

The [README](README.md#exposing-binbash-to-the-internet) covers the two non-optional steps: terminate HTTPS with a reverse proxy, and set `bind_address = "127.0.0.1"` so nobody can bypass it. This section is the rest.

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
