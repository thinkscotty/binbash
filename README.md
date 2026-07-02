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

All configuration is via environment variables:

| Var | Default | Purpose |
|---|---|---|
| `BINBASH_PORT` | `8080` | HTTP listen port |
| `BINBASH_PASSWORD` | *(required)* | Initial login password, used to bootstrap the account on first run. Change it in-app via "Change password" afterward — the database, not this variable, is the source of truth from then on. |
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

Set `BINBASH_AI_BASE_URL` (plus `BINBASH_AI_API_KEY` and `BINBASH_AI_MODEL` as your provider requires) to enable AI tagging. Once configured, the **Items** page shows a "Tag up to 25 items" button that asks the AI for search-friendly keyword suggestions (synonyms, alternate names, categories) for items that haven't been tagged yet, and appends them to each item's existing keywords — it never edits or removes what's already there. Once an item is tagged it's skipped by future runs, even if it ended up with zero suggestions; re-click the button to work through a larger backlog in batches of 25.

## Building

```sh
go build -o binbash .
```

Produces a single static binary with no runtime dependencies.

## License

MIT — see [`LICENSE`](LICENSE).
