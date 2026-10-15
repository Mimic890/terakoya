# terakoya

A Telegram bot that serves a wiki-like catalog of school material,
starting with geometry for grades 7–11.

The catalog is a git repository of plain Markdown files that reads well in
Obsidian, on GitHub or in any Markdown editor. The bot renders articles as
Telegram rich messages: formulas, tables, images and links between
articles included. See [docs/content-guide.md](docs/content-guide.md) for
the content format.

## Status

- [x] Content loader, Markdown → Telegram rich message converter, `lint`
- [x] Read-only navigation: subjects → grades → topics, deep links, `.md` download
- [ ] Edit suggestions as pull requests, git host webhooks, content sync
- [ ] Ranks for merged suggestions, `/download` of the whole catalog

## Running

You need a bot token from [@BotFather](https://t.me/BotFather). The bot
uses `sendRichMessage`, so it needs Bot API 10.2 or later (the official
`api.telegram.org` server has it).

### Docker Compose

```sh
cp .env.example .env        # set TERAKOYA_BOT_TOKEN and TERAKOYA_REPO_URL
mkdir -p data content
docker compose up -d --build
```

`./data` holds the SQLite database and `./content` the checkout of the
content repository. Both are bind mounts. If `./content` is empty and
`TERAKOYA_REPO_URL` is set, the bot clones the repository on start.
The container runs as `$UID:$GID` (default `1000:1000`), which should own
both directories.

### From source

Go 1.27 or later; no C toolchain is needed.

```sh
go build ./cmd/terakoya
TERAKOYA_BOT_TOKEN=… TERAKOYA_REPO_DIR=examples ./terakoya
```

This serves the sample articles in `examples/content`.

## Commands

```
terakoya [serve] [-config file]     run the bot (default)
terakoya lint [-strict] <dir>       validate a content directory
```

`lint` prints one `path:line: severity: message` line per problem and exits
with status 1 if there are errors (or warnings, with `-strict`). It checks
directory structure, frontmatter, ID uniqueness, `[[links]]`, images, math
delimiters and Telegram message limits. The Docker image includes it, so a
content repository can run it in CI without a Go toolchain:

```yaml
# .github/workflows/lint.yml in the content repository
on: [push, pull_request]
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: >
          docker run --rm -v "$PWD:/work:ro" -w /work
          ghcr.io/mimic890/terakoya:latest lint content
```

Replace the image with wherever you publish it; a Forgejo Actions runner
with Docker uses the same step. Locally:

```sh
docker run --rm -v "$PWD:/work:ro" -w /work terakoya:dev lint content
```

## Configuration

Options come from environment variables and an optional YAML config file
(`-config path` or `TERAKOYA_CONFIG`). Environment variables override the
file; empty variables are ignored. See
[terakoya.example.yml](terakoya.example.yml) for the file format.

| Variable | File key | Default | Description |
|----------|----------|---------|-------------|
| `TERAKOYA_BOT_TOKEN` | `bot_token` | — | Telegram bot token. Required. |
| `TERAKOYA_BOT_USERNAME` | `bot_username` | from `getMe` | Bot username, used in deep links. |
| `TERAKOYA_REPO_DIR` | `repo_dir` | `./content` (`/srv/content` in Docker) | Local checkout of the content repository. |
| `TERAKOYA_REPO_URL` | `repo_url` | — | Content repository to clone when `repo_dir` is empty. Without it, `repo_dir` is used as is. |
| `TERAKOYA_REPO_BRANCH` | `repo_branch` | `main` | Branch to clone. |
| `TERAKOYA_CONTENT_SUBDIR` | `content_subdir` | `content` | Catalog directory inside the repository. |
| `TERAKOYA_DATA_DIR` | `data_dir` | `./data` (`/srv/data` in Docker) | Directory for the SQLite database. |
| `TERAKOYA_CACHE_CHAT_ID` | `cache_chat_id` | — | Chat where new images are uploaded once to get a `file_id` (see below). |
| `TERAKOYA_RANKS` | `ranks` | `kōhai:0,senpai:3,sensei:10` | Rank titles and the number of merged suggestions each starts at. The first must start at 0. |
| `TERAKOYA_LOG_LEVEL` | `log_level` | `info` | `debug`, `info`, `warn` or `error`. |
| `TERAKOYA_CONFIG` | — | — | Path to the YAML config file. |

### Images and the file_id cache

Images are uploaded to Telegram once; afterwards the bot reuses the
`file_id`, cached by the SHA-256 of the file, so a changed image is
uploaded again automatically.

Without `TERAKOYA_CACHE_CHAT_ID`, a new image is attached to the first
article message that uses it, and its `file_id` is read back from the sent
message. With it, the bot first uploads new images to that chat (for
example, a private channel where the bot is an admin) and sends articles
with `file_id`s only. Use the cache chat if you see repeated uploads in the
logs.

## Development

```sh
go test ./...
go test ./internal/render -update   # rewrite converter golden files
go run ./cmd/terakoya lint examples/content
```

Converter tests compare the output for `internal/render/testdata/content`
and `examples/content` with the files in `internal/render/testdata/golden`.
