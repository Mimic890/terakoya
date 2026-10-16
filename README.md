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
- [x] Read-only navigation: subjects → grades → topics, deep links, file download
- [x] Suggestions from Telegram as pull requests and issues (GitHub or Forgejo), git host webhooks, content sync
- [ ] Rank polish, `/download` of the whole catalog, Telegram webhook mode

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
| `TERAKOYA_GIT_HOST` | `git_host` | — | `github` or `forgejo`. Enables suggestions, git webhooks and sync; requires `TERAKOYA_REPO_URL`. |
| `TERAKOYA_GIT_API_URL` | `git_api_url` | `https://api.github.com` for GitHub | Base URL of the host. Required for Forgejo, e.g. `https://git.hyperchat.ru`. |
| `TERAKOYA_GIT_REPO` | `git_repo` | — | Content repository as `owner/name`. |
| `TERAKOYA_GIT_TOKEN` | `git_token` | — | Access token, used for the API and for git over HTTPS. See below for scopes. |
| `TERAKOYA_GIT_WEBHOOK_SECRET` | `git_webhook_secret` | — | Secret shared with the webhook; deliveries with a wrong signature are rejected. |
| `TERAKOYA_GIT_AUTHOR_NAME` | `git_author_name` | `terakoya` | Committer name of suggestion commits. The author is the Telegram user. |
| `TERAKOYA_GIT_AUTHOR_EMAIL` | `git_author_email` | `terakoya@localhost` | Committer email. |
| `TERAKOYA_ACCEPTED_LABEL` | `accepted_label` | `accepted` | Label that marks an error report as accepted (see below). Created on start if missing. |
| `TERAKOYA_HTTP_ADDR` | `http_addr` | `:8080` | Address of the HTTP server for git webhooks (`POST /webhooks/git`, `GET /healthz`). |
| `TERAKOYA_SYNC_INTERVAL` | `sync_interval` | `5m` | How often to pull the content repository and check open suggestions, in case a webhook was missed. `0` disables. |
| `TERAKOYA_SUGGEST_COOLDOWN` | `suggest_cooldown` | `1m` | Minimum time between two suggestions of one user. |
| `TERAKOYA_SUGGEST_MAX_OPEN` | `suggest_max_open` | `5` | Maximum unresolved suggestions per user. |
| `TERAKOYA_SUGGEST_MAX_BYTES` | `suggest_max_bytes` | `262144` | Maximum size of a suggested text. |
| `TERAKOYA_IMAGE_MAX_BYTES` | `image_max_bytes` | `5242880` | Maximum size of a picture attached to a suggestion. |
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

## Suggestions

With `TERAKOYA_GIT_HOST` set, users can suggest changes without knowing
anything about Markdown, LaTeX, files or git. The bot never shows them
file names, frontmatter or syntax; everything is buttons and plain text.

Under an article, «Предложить правку» offers two options:

- **«Исправить текст»**: the bot sends the article as plain readable text:
  no Markdown markers, formulas as Unicode (`∠A + ∠B = 180°`), pictures as
  «[картинка]». Long articles are offered section by section. The user
  copies the text, fixes it and sends it back; pictures can be added along
  the way, with a caption saying where they go. After «Отправить», the bot
  merges the text into the Markdown source paragraph by paragraph:
  untouched paragraphs, table rows and list items keep their exact source,
  edited ones come in as the user's text with formulas, links and bold text
  put back wherever their plain form survived. Paragraphs that were moved
  keep their source. When the text cannot be matched reliably (for
  example, it was pasted twice and both copies were edited), it replaces
  the whole section and the pull request says so. If the text is much
  shorter than the section, the user is asked to confirm first.
- **«Сообщить об ошибке»**: the user describes the problem in their own
  words, optionally with photos. This opens an **issue** for a maintainer
  to fix by hand.

In a grade's topic list, «Добавить тему» asks for the title, then the text,
then pictures. The file name, order and frontmatter are generated.

Pull requests are created as follows: the bot remembers the content commit
the user was looking at, commits the change with git plumbing on a new
branch `suggest/<tg_id>-<slug>-<short_id>` whose parent is that commit, and
opens a pull request. If `main` changed the same lines in the meantime, the
pull request shows a conflict instead of silently overwriting them.
Pictures go to the grade's `assets/` directory, not yet referenced from the
article. The pull request lists what to check: the paragraphs that came in
as plain text (before and after), merge notes, the pictures with their
captions and previews, the text exactly as sent, and lint findings, which
users never see.

Outcomes:

- A merged pull request, or an issue closed with the `accepted` label
  (`TERAKOYA_ACCEPTED_LABEL`), counts toward the user's rank, and they get
  a thank-you message with a button to open the topic.
- A pull request closed without merging, or an issue closed without the
  label, sends the user the last review comment. For issues, the bot waits
  two minutes after the close, so closing first and labeling right after
  is fine. Adding the label to an issue that was already reported as not
  accepted still counts it, once.
- Every `TERAKOYA_SYNC_INTERVAL`, the bot also asks the host about each open
  suggestion, so outcomes are noticed even when a webhook delivery is lost.

The checkout in `TERAKOYA_REPO_DIR` belongs to the bot: syncing resets it
to the remote branch. The bot refuses to use a checkout whose `origin` is
not `TERAKOYA_REPO_URL`. Use an HTTPS `TERAKOYA_REPO_URL`: links in pull
requests and issues are built from it.

### GitHub

- Token: a fine-grained personal access token for the content repository
  with **Contents**, **Pull requests** and **Issues: read and write**.
- Webhook (repository → Settings → Webhooks): payload URL
  `https://<your host>/webhooks/git`, content type `application/json`,
  secret = `TERAKOYA_GIT_WEBHOOK_SECRET`, events **Pushes**,
  **Pull requests** and **Issues**.
- GitHub has no API for issue attachments, so pictures of error reports
  are pushed to a `report/…` branch and linked from the issue.

### Forgejo

- Token (user settings → Applications): scopes **repository: read and
  write** and **issue: read and write** (error reports, their pictures and
  the label), for a user with write access to the content repository.
- Webhook (repository → Settings → Webhooks → Forgejo): target URL
  `https://<your host>/webhooks/git`, content type `application/json`,
  secret = `TERAKOYA_GIT_WEBHOOK_SECRET`, custom events **Push**,
  **Pull request**, **Issues** and **Issue label**.

The webhook server listens on `TERAKOYA_HTTP_ADDR`; put it behind a
reverse proxy with TLS. Without working webhooks, everything still
happens, only up to `TERAKOYA_SYNC_INTERVAL` later: the content syncs and
open suggestions are checked on that schedule. A label added to an
already closed issue is only noticed through the webhook.

## Development

```sh
go test ./...
go test ./internal/render -update   # rewrite converter golden files
go run ./cmd/terakoya lint examples/content
```

Converter tests compare the output for `internal/render/testdata/content`
and `examples/content` with the files in `internal/render/testdata/golden`.
