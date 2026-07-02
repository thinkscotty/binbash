# binbash

A simple, fast, self-hosted home inventory app for tracking what's in your storage bins — built for speed of entry and dead-simple search, not for feature sprawl.

See [`binbash_initial_idea.md`](binbash_initial_idea.md) for the motivating problem, [`ARCHITECTURE.md`](ARCHITECTURE.md) for the technical decisions, and [`PLAN.md`](PLAN.md) for the feature roadmap.

## Running locally

Requires Go 1.22+.

```sh
export BINBASH_PASSWORD=changeme
go run .
```

Then visit http://localhost:8080 and sign in with the password above.

### Configuration

Configure binbash with a TOML file, environment variables, or both. A file is usually easier to keep around; env vars always win if both set the same value, so a single one can still be overridden at runtime (e.g. Docker's `-e`) without touching the file.

**Config file**: copy [`binbash.example.toml`](binbash.example.toml) to `binbash.toml` in the directory you run the binary from, and fill in what you need — every field is optional and commented. To use a file at a different path or name, point `BINBASH_CONFIG` at it instead of relying on that default lookup.

**Environment variables** (each overrides its config-file counterpart):

| Var | Default | Purpose |
|---|---|---|
| `BINBASH_CONFIG` | `./binbash.toml` | Path to the config file. Not itself overridable by the file, for obvious reasons. |
| `BINBASH_PORT` | `8080` | HTTP listen port |
| `BINBASH_PASSWORD` | *(required, file or env)* | Initial login password, used to bootstrap the account on first run. Change it in-app via "Change password" afterward — the database, not this variable, is the source of truth from then on. |
| `BINBASH_DB_PATH` | `./data/binbash.db` | SQLite file location |
| `BINBASH_AI_BASE_URL` | *(unset = AI tagging disabled)* | OpenAI-compatible endpoint. Include any path prefix the provider needs (e.g. `https://api.openai.com/v1`) — `/chat/completions` is appended directly. |
| `BINBASH_AI_API_KEY` | *(unset)* | API key for the AI endpoint |
| `BINBASH_AI_MODEL` | *(unset)* | Model name to request |
| `BINBASH_AI_TAG_COUNT` | `3` | Max AI-suggested tags per item (0–8; 0 keeps the feature on but generates no tags) |
| `BINBASH_AI_TAG_BREADTH` | `moderate` | How closely related suggested tags should be: `narrow`, `moderate`, or `broad` |
| `BINBASH_AUTO_BACKUP_DIR` | *(unset = disabled)* | Directory to write automatic CSV backups to |

## Backing up

Visit **Backup** in the nav to download a CSV of your whole inventory (one row per item, with its bin's name/category/description), or to import a CSV back in. Importing adds items to what you already have by default; check "Replace existing inventory" to wipe and reload from the file instead. The search page nudges you to back up once you've added 50+ items since the last one. Set `BINBASH_AUTO_BACKUP_DIR` to have binbash write one of these CSVs automatically once that threshold is crossed, keeping the 5 most recent.

## AI tagging

Set `BINBASH_AI_BASE_URL` (plus `BINBASH_AI_API_KEY` and `BINBASH_AI_MODEL` as your provider requires) to enable AI tagging. Once configured, the **Items** page shows a "Tag up to 25 items" button that asks the AI for search-friendly keyword suggestions (synonyms, alternate names, categories) for items that haven't been tagged yet, and appends them to each item's existing keywords — it never edits or removes what's already there. Once an item is successfully tagged it's skipped by future runs; an item whose response came back with no usable tags is left untagged so a later run retries it, rather than being marked done with nothing. Re-click the button to work through a larger backlog in batches of 25.

Any OpenAI-compatible endpoint works, including reasoning models (Qwen3, DeepSeek-R1, etc.) — those spend part of their reply "thinking" before answering, so binbash requests a generous token budget to let them finish and still return tags.

## Building

```sh
go build -o binbash .
```

Produces a single static binary with no runtime dependencies.

## Deployment

binbash is a single static binary — no runtime dependencies, no separate database process to run. A few ways to run it in production:

### Prebuilt binary

Grab a binary for your platform from the [Releases page](https://github.com/thinkscotty/binbash/releases) (Linux amd64/arm64, macOS, Windows), or build it yourself per [Building](#building) above. Run it directly, or under a process supervisor — see the systemd example below.

### systemd (Linux)

A sample unit file is at [`deploy/binbash.service`](deploy/binbash.service). Typical setup:

```sh
sudo useradd -r -s /usr/sbin/nologin binbash
sudo mkdir -p /opt/binbash
sudo cp binbash /opt/binbash/
sudo cp deploy/binbash.service /etc/systemd/system/
echo "BINBASH_PASSWORD=changeme" | sudo tee /opt/binbash/.env
sudo chown -R binbash:binbash /opt/binbash
sudo systemctl enable --now binbash
```

Prefer a config file over the `.env`? Since the unit's `WorkingDirectory` is `/opt/binbash`, dropping a `binbash.toml` there (`sudo cp binbash.example.toml /opt/binbash/binbash.toml`, then edit it) is picked up automatically with no changes to the unit file.

### Docker

```sh
docker build -t binbash .
docker run -d \
  --name binbash \
  -p 8080:8080 \
  -e BINBASH_PASSWORD=changeme \
  -v ./data:/data \
  binbash
```

The image is a convenience wrapper around the same binary, not the primary deployment path. Mount `/data` to persist the SQLite database (and any auto-backups, if `BINBASH_AUTO_BACKUP_DIR` is also set to somewhere under `/data`).

### Put a reverse proxy in front

binbash only speaks plain HTTP and is protected by a single shared password — **don't expose it directly to the internet**. Put a reverse proxy (Caddy, nginx, Traefik) in front to terminate HTTPS, or keep it on a private/VPN-only network. Without HTTPS, the login password travels in plaintext.

## License

MIT — see [`LICENSE`](LICENSE).
