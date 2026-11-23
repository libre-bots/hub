# Libre Bots hub

The hub is a gRPC service that owns the Postgres database for a family of Discord bots. Bots do not talk to the
database; they call the hub with a per-bot token. It keeps which bot is in which guild, each guild's language,
pending deletion requests and statistics, and runs the privacy operations, in one place. The hub deletes what it
stores for a guild 30 days after the bot left it, and writes daily figures. The design is described in the
[architecture document](https://github.com/libre-bots/.github/blob/main/docs/architecture.md).

## Run it

```sh
docker compose up
```

Postgres runs inside the compose project. The hub listens on `localhost:50051` (gRPC, plaintext, reflection on).
The compose file registers one bot, `example`, whose token is `dev-token`. The compose file is for development: its
token is public, it uses plaintext and it leaves reflection on. When you work on the code, use
`docker compose up --build`.

## Configuration

| Variable | Default | Rule |
|---|---|---|
| `DATABASE_URL` | none | Postgres connection string. Set this or `DATABASE_URL_FILE`, not both. |
| `DATABASE_URL_FILE` | none | Path to a file holding the connection string. |
| `HUB_LISTEN_ADDR` | `:50051` | Address to listen on. |
| `HUB_BOT_TOKENS` | empty | Comma-separated `name=sha256` entries. Names match `^[a-z][a-z0-9_]{1,31}$` and are not `all`; hashes are 64 lower-case hex characters; names and hashes must be unique. |
| `HUB_REFLECTION` | `false` | `true` or `false`. Turns gRPC reflection on. |
| `HUB_TLS_CERT_FILE` | none | Certificate file. Requires `HUB_TLS_KEY_FILE`. |
| `HUB_TLS_KEY_FILE` | none | Private key file. Requires `HUB_TLS_CERT_FILE`. |

The binary has four subcommands: `serve` (the default), `token`, `health` and `version`.

## Bot tokens

```sh
docker run --rm ghcr.io/libre-bots/hub token
```

prints a new token and its SHA-256 hash. Put the hash into `HUB_BOT_TOKENS` as `name=hash`. Give the token to the
bot; it sends the token with every call. The hub only stores the hash.

## Development

You need [mise](https://mise.jdx.dev) and Docker.

```sh
mise install
```

- `make generate`: regenerate the Go, SQL and Python code from `proto/` and the queries
- `make lint`: `go vet`, `buf lint`, `sqlc diff` and `actionlint`
- `make test`: run the Go tests (they start Postgres with Docker)
- `make test-race`: the same with the race detector
- `make build`: build `bin/hub`
- `make python-lint`: ruff and pyright for the Python client
- `make python-test`: the Python client tests
- `make e2e`: end-to-end tests against a hub started with `docker compose up --build --wait`

## Python client

```sh
pip install "libre-hub-client @ git+https://github.com/libre-bots/hub@v0.1.0#subdirectory=clients/python"
```

```python
import asyncio

from libre.hub.v1 import stats_pb2, stats_pb2_grpc
from libre_hub_client import connect


async def main() -> None:
    async with connect("localhost:50051", "dev-token") as channel:
        stats = await stats_pb2_grpc.StatsServiceStub(channel).GetStats(stats_pb2.GetStatsRequest())
        print(stats.total.guild_count)


asyncio.run(main())
```

## Hosting it yourself

- Keep the Postgres volume on encrypted storage. The database holds guild ids, their settings and the user ids of
  pending deletion requests.
- Never use `dev-token` outside development. Create tokens with `hub token`.
- Set `HUB_TLS_CERT_FILE` and `HUB_TLS_KEY_FILE` so that tokens do not cross the network in plaintext, and leave
  `HUB_REFLECTION` off.
