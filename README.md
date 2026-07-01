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
| `BINBASH_PASSWORD` | *(required)* | Login password |
| `BINBASH_DB_PATH` | `./data/binbash.db` | SQLite file location |
| `BINBASH_AI_BASE_URL` | *(unset = AI tagging disabled)* | OpenAI-compatible endpoint |
| `BINBASH_AI_API_KEY` | *(unset)* | API key for the AI endpoint |
| `BINBASH_AI_MODEL` | *(unset)* | Model name to request |

## Building

```sh
go build -o binbash .
```

Produces a single static binary with no runtime dependencies.

## License

MIT — see [`LICENSE`](LICENSE).
