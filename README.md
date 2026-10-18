# terakoya

A Telegram bot that serves a wiki-like catalog of school material,
starting with geometry for grades 7–11.

The material is an Obsidian vault kept in a git repository: subjects,
grades and numbered topics are folders, each topic has a main page and
folders of theorems, tasks and so on, with SVG drawings in `_assets/`.
There are no config files or frontmatter; everything comes from folder
names and note text. The bot renders notes as Telegram rich messages
(formulas, tables, drawings, links between notes), splitting long notes
into several messages. See [docs/content-guide.md](docs/content-guide.md)
(in Russian, for content authors) for the format.

## Status

- [x] Obsidian vault loader, Markdown → Telegram rich message converter with SVG drawings and long-note splitting, `lint`
- [x] Navigation: subjects → grades → topics → notes, deep links, note download, `/download` of the whole vault
- [x] Suggestions from Telegram as pull requests and issues (GitHub or Forgejo), git host webhooks, content sync
- [x] Edit access granted by admins, bottom keyboard, pinned notes, account page, ranks
- [x] Multi-arch Docker image on ghcr.io
- [ ] Telegram webhook mode

## Running

You need a bot token from [@BotFather](https://t.me/BotFather). The bot
uses `sendRichMessage`, so it needs Bot API 10.2 or later (the official
`api.telegram.org` server has it).

### Docker

The image is published to `ghcr.io/mimic890/terakoya` for amd64 and
arm64: `latest` follows `main`, `1.2.3` and `1.2` are releases, and every
build is also tagged `sha-<commit>`. It includes resvg and the DejaVu
fonts. Save this as `docker-compose.yml`:

```yaml
services:
  terakoya:
    image: ghcr.io/mimic890/terakoya:latest
    restart: unless-stopped
    # Run as the owner of ./data and ./vault.
    user: "1000:1000"
    env_file: .env
    ports:
      # Git host webhooks (POST /webhooks/git); put a TLS reverse proxy in
      # front, or use "127.0.0.1:8080:8080" when it runs on the same host.
      - "8080:8080"
    volumes:
      - ./data:/srv/data
      - ./vault:/srv/vault
```

Then:

```sh
cp .env.example .env        # set TERAKOYA_BOT_TOKEN, TERAKOYA_REPO_URL, TERAKOYA_ADMIN_IDS
mkdir -p data vault
docker compose up -d
docker compose logs -f
```

`./data` holds the SQLite database and `./vault` the checkout of the
content repository. If `./vault` is empty and `TERAKOYA_REPO_URL` is set,
the bot clones the repository on start. Do not set `TERAKOYA_REPO_DIR` or
`TERAKOYA_DATA_DIR` in `.env`: the image points them at the mounts.

To update:

```sh
docker compose pull && docker compose up -d
```

The image is public. If you publish your own build as a private image,
log in once on the server with a personal access token (classic) that has
the `read:packages` scope:

```sh
docker login ghcr.io -u <github user>
```

To build the image from a checkout instead, use the `docker-compose.yml`
in this repository: `docker compose up -d --build`.

### From source

Go 1.27 or later; no C toolchain is needed.

```sh
go build ./cmd/terakoya
TERAKOYA_BOT_TOKEN=… TERAKOYA_REPO_DIR=examples/vault ./terakoya
```

This serves the sample vault in `examples/vault`. SVG drawings need
[resvg](https://github.com/linebender/resvg) (`cargo install resvg`) and
the DejaVu fonts; the Docker image has both.

## Commands

```
terakoya [serve] [-config file]     run the bot (default)
terakoya lint [-strict] <vault>     validate a vault
```

In Telegram, `/start` sends a greeting that sets the keyboard at the
bottom of the chat, and the subject list. The keyboard stays: «Предметы»
opens the subject list, «Закреплённые» the user's pinned notes, and
«Аккаунт» a page with their rank, how many accepted suggestions the next
rank needs, accepted and open suggestions (and «Уровень прав: admin» for
admins). From there, «Список правок» lists their suggestions under
«На рассмотрении», «Приняты» and «Отклонены», separated by dividers, with
the reviewer's comment on rejected ones; titles of notes in the vault
open them. Both pages are rich messages, split like long notes. A tap on
the keyboard shows the new menu first; the tap itself and the previous
menu are deleted afterwards, in the background, so the chat does not fill
up. Tapping the button of the menu that is already the last message only
deletes the tap. Typed commands are deleted once answered. Every note has
«Закрепить» / «Открепить» (at most 30 pins); a note opened from the
pinned list goes back to it with «Назад». Pins of notes removed from the
vault disappear from the list. `/download` sends the whole vault as a zip
(without hidden files such as `.obsidian`).

The interface has no emoji of its own.

Admins also have `/access`, which lists who has edit access, and
`/revoke <id or @username>`. For everyone else these commands do nothing.

`lint` prints one `path:line: severity: message` line per problem and exits
with status 1 if there are errors (or warnings, with `-strict`). It checks
the folder structure, links and images (broken or ambiguous), duplicate
topic numbers, notes without a title, unexpected files, SVGs that fail to
rasterize, math delimiters and Telegram message limits, and lists notes
that will be sent in several parts as `info`. The Docker image includes it, so a
content repository can run it in CI without a Go toolchain:

```yaml
# .github/workflows/lint.yml in the vault repository
on: [push, pull_request]
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: >
          docker run --rm -v "$PWD:/work:ro" -w /work
          ghcr.io/mimic890/terakoya:latest lint .
```

A Forgejo Actions runner with Docker uses the same step. Locally:

```sh
docker run --rm -v "$PWD:/work:ro" -w /work ghcr.io/mimic890/terakoya lint .
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
| `TERAKOYA_REPO_DIR` | `repo_dir` | `./content` (`/srv/vault` in Docker) | Local checkout of the content repository. |
| `TERAKOYA_REPO_URL` | `repo_url` | — | Content repository to clone when `repo_dir` is empty. Without it, `repo_dir` is used as is. |
| `TERAKOYA_REPO_BRANCH` | `repo_branch` | `main` | Branch to clone. |
| `TERAKOYA_DATA_DIR` | `data_dir` | `./data` (`/srv/data` in Docker) | Directory for the SQLite database. |
| `TERAKOYA_CACHE_CHAT_ID` | `cache_chat_id` | — | Chat where new images are uploaded once to get a `file_id` (see below). |
| `TERAKOYA_RANKS` | `ranks` | `Novice:0,Pupil:1,Student:3,Scholar:7,Mentor:15,Teacher:30,Master:60,Sage:100` | Rank titles and the number of merged suggestions each starts at. The first must start at 0. |
| `TERAKOYA_ADMIN_IDS` | `admin_ids` | — | Telegram user IDs of the admins, comma-separated. They grant edit access (see below). Without admins, nobody can edit; error reports still work. |
| `TERAKOYA_LOG_LEVEL` | `log_level` | `info` | `debug`, `info`, `warn` or `error`. |
| `TERAKOYA_RESVG` | `resvg` | `resvg` | The resvg binary that rasterizes SVG drawings. |
| `TERAKOYA_FONTS_DIR` | `fonts_dir` | `/usr/share/fonts/dejavu` or `/usr/share/fonts/truetype/dejavu` | DejaVu fonts for the drawings' labels; system fonts are not used, so drawings look the same everywhere. |
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
uploaded again automatically. SVG drawings are rasterized with resvg at
twice their size on a white background, only when their PNG is not cached
yet: the cache key is the hash of the SVG source plus the rendering
settings, so an unchanged drawing is never converted or uploaded again. If
a drawing cannot be converted, the note is sent without it and the error
is logged (lint reports it too).

Without `TERAKOYA_CACHE_CHAT_ID`, a new image is attached to the first
message that uses it, and its `file_id` is read back from the sent
message. With it, the bot first uploads new images to that chat (for
example, a private channel where the bot is an admin) and sends notes
with `file_id`s only. Use the cache chat if you see repeated uploads in the
logs.

## Suggestions

With `TERAKOYA_GIT_HOST` set, users can suggest changes without knowing
anything about Markdown, LaTeX, files or git. The bot never shows them
file names, frontmatter or syntax; everything is buttons and plain text.

Anyone can report an error, but editing text and adding topics or notes
need **edit access**. A user without it who taps «Исправить текст»,
«Добавить тему» or «Добавить теорему, задачу…» is offered to send a
request. Every admin (`TERAKOYA_ADMIN_IDS`) gets it
with the user's name, @username, ID and accepted edits and error reports,
and «Разрешить» / «Отклонить». The first admin to decide wins; every
admin's copy then shows who decided. The user is told either way. One
request can be pending per user; after a denial, or after `/revoke`, the
user can ask again in 7 days. A revoke also ends an edit the user has open.
Admins always have access. An admin must have opened the bot once to get
requests.

Under a note, «Предложить правку» offers two options, and a third under a
topic's main page:

- **«Исправить текст»**: the bot sends the note as plain readable text:
  no Markdown markers, formulas as Unicode (`∠A + ∠B = 180°`), pictures as
  «[картинка]», one line per line as in Obsidian, without the back-link
  block at the end (which is kept). Long notes are offered section by
  section. The user
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
- **«Добавить теорему, задачу…»**: the user picks the kind (theorem, axiom,
  definition, task, corollary, or another kind folder the topic has), then
  writes the title, the text and adds pictures. The note goes to
  `<kind folder>/<title>.md` with a back-link to the topic, and the same
  pull request links it from the main page: right after the last link to a
  note of that kind, or under a new `## Теоремы` (etc.) section. For a
  corollary the user picks its theorem; the note goes to that theorem's
  `следствия/` folder and the link is nested under the theorem's.

In a grade's topic list, «Добавить тему» asks for the title, then the text,
then pictures. It creates `<number> <title>/<title>.md`, numbered after
the last topic of the grade and padded like the others. File names are the
title as typed, minus characters Windows or macOS do not allow; a title
whose file name already exists in the vault is refused, so links by name
keep working.

Pull requests are created as follows: the bot remembers the content commit
the user was looking at, commits the change with git plumbing on a new
branch `suggest/<tg_id>-<slug>-<short_id>` whose parent is that commit, and
opens a pull request. If `main` changed the same lines in the meantime, the
pull request shows a conflict instead of silently overwriting them.
Pictures go to the topic's `_assets/` folder, not yet referenced from the
note. The pull request lists what to check: the paragraphs that came in
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
go run ./cmd/terakoya lint examples/vault
```

Every push to `main` and every `v*` tag runs
[.github/workflows/image.yml](.github/workflows/image.yml): the tests,
then the image is built natively on amd64 and arm64 runners, lints
`examples/vault` and is pushed to ghcr.io.

Converter tests compare the output for `internal/render/testdata/vault`
and `examples/vault` with the files in `internal/render/testdata/golden`.
The SVG test runs resvg when it is installed (`TERAKOYA_RESVG` and
`TERAKOYA_FONTS_DIR` point to it) and is skipped otherwise.
