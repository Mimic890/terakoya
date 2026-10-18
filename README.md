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
- [x] Error reports from Telegram as issues (GitHub or Forgejo), git host webhooks, content sync
- [x] Bottom keyboard, pinned notes, account page, ranks, leaderboard, nicknames, admin broadcasts
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
bottom of the chat, and the subject list. The keyboard stays, two by two:
«Предметы» opens the subject list, «Закреплённые» the user's pinned
notes, «Аккаунт» a page with their rank, how many accepted reports the
next rank needs («ещё 1 принятая правка», «ещё 2 принятые правки», «ещё 5
принятых правок»; the UI counts accepted reports as «правки») and which it is
(«Следующий ранг: Student», with its custom emoji; at the top rank just
«Это высший ранг»), «Принято правок: N» and «На рассмотрении: N»
(and «Уровень прав: admin» for admins), and «Настройки» (see
[below](#settings-nickname-and-the-top)). From «Аккаунт», «Мои
правки» lists their reports under «На рассмотрении», «Приняты» and
«Отклонены», separated by dividers, with the reviewer's comment on
rejected ones; titles of notes in the vault open them. Edits and topics
suggested through earlier versions of the bot stay in the list, but only
reports count toward the rank. «Топ» there, or `/top`, shows the
[leaderboard](#top), and «Ранги», or `/ranks`, [the ladder](#ranks). These pages are rich messages, split like long notes. A tap on
the keyboard shows the new menu first; the tap itself and the previous
menu are deleted afterwards, in the background, so the chat does not fill
up. Tapping the button of the menu that is already the last message only
deletes the tap. Typed commands are deleted once answered. Every note has
«Закрепить» / «Открепить» (at most 30 pins); a note opened from the
pinned list goes back to it with «Назад». Pins of notes removed from the
vault disappear from the list. `/download` sends the whole vault as a zip
(without hidden files such as `.obsidian`).

The interface has no emoji of its own.

Admins also have `/emoji_setup` (see
[the settings file](#settings-file-ranks-custom-emoji-topic-list-top)),
[`/broadcast`](#broadcasts) and `/nick_reset <id or @username>`. For
everyone else these commands do nothing. The command menu shows
`/start`, `/download`, `/top`, `/ranks` and `/cancel` to everyone, and the admin
commands only in the admins' chats (set on start; an admin who has never
opened the bot gets them after the next restart).

### Ranks

«Ранги» lists the ladder in order, each rank with its custom emoji and
threshold («Student — от 3 принятых правок»); the user's own rank is bold
and marked «· ваш ранг». Ranks the user has reached and the next one are
shown in full; with `ranks.hide_future: true` (the default) later ones
read «??? — от 7 принятых правок», without name or emoji. Admins see
every rank, and how to change them. It has «Назад» (to «Аккаунт») and
«Закрыть».

### Top

«Топ участников» ranks users by accepted reports; of two with the same
count, whoever reached it first is higher. Users without accepted
reports are not listed. A viewer outside the list gets a last line with
their own place, «Ваше место: 25 — 3 принятые правки». It has
«Назад» (to «Аккаунт») and «Закрыть». The `top:` section of
`settings.yml` sets how it looks; a line reads, by `rank_style`:

| `rank_style` | line |
|---|---|
| `none` | 1. name — 12 |
| `title` | 1. name — Scholar · 12 |
| `emoji` (default) | 🥇 name 🎓 · 12 |
| `title_emoji` | 🥇 name — 🎓 Scholar · 12 |

The first `medals` places show their medal (`/emoji_setup top:1` …
`top:5`) instead of the number, while one is set. A rank without a
custom emoji shows its title instead (`emoji` and `title_emoji` then
read like `title`). Without `show_count` the count goes.

### Settings: nickname and the top

«Настройки» has two screens in one menu message, edited in place. The
main one shows «Ник: …» and «Показывать меня в топе: да/нет», with «Ник»,
the toggle «Топ: показывать ✓» / «Топ: скрыт», and «Закрыть». «Ник»
replaces it with «Сейчас: Аня (из Telegram)» or «Сейчас: Ёжик (свой)»,
«Ник видно в топе и в приветствии.», the choice «Из Telegram» /
«Свой ник» with a ✓ on the active one, and «Назад». «Из Telegram» clears
the nickname (tapped while active, it does nothing); «Свой ник» asks for
one, and while active asks for a new one («Сейчас: Ёжик. Пришлите новый
ник.»). Saving or «Отмена» returns to the «Ник» screen. A user who hides from the top
is left out of it for everyone else, and still sees their own place.

Where the bot addresses the user (the /start greeting «Привет, …!», the
«Аккаунт» heading, «Ник: …» in «Настройки») it uses the nickname, else
the Telegram first name, never the @username. Where people must be told
apart (the top, admin replies, report issues) it uses the nickname, else
the @username, else the first name. A nickname is 3 to 20 characters: Latin or Cyrillic
letters, digits, `_`, `-` and single spaces, not at either end. It must
be unique ignoring case, look-alike letters and separators (with «Маша»
taken, «Mаша» with a Latin M, «Ма_ша» or «Ма ша» are too), and is
refused if it contains a rude word from
a short built-in list, «admin», «админ», «terakoya», the bot's username,
or an admin's Telegram name or username, look-alike Latin and Cyrillic
letters included (an admin may use their own name). A refused nickname
gets the reason and the question again. `/nick_reset <id or @username>`
puts a user back on their Telegram name and tells them «Ваш ник сброшен
администратором.»

### Broadcasts

`/broadcast` asks an admin for the message: text, optionally with one
photo, with Telegram formatting (bold, italic, underline,
strikethrough, spoilers, code, links, quotes, custom emoji), which is
converted to rich Markdown. The bot shows a preview exactly as users will
get it and asks «Отправить всем? Получателей: N» with «Да, отправить»,
«Изменить» (asks for the message again) and «Нет» (cleans up like
«Отмена»). Recipients are everyone who has started the bot, except admins
and users who blocked it.

Sending runs in the background, 40 ms apart (under Telegram's 25
messages a second). After a 429 the bot waits as long as Telegram says
and sends again; a user who answers 403 has blocked the bot and is
skipped from then on, until they use the bot again. The admin gets a
progress message, updated every 10 seconds, that ends with how many were
sent, blocked and failed; the preview stays in their chat as a record.
Broadcasts and their counts are stored in the database. A restart in the
middle stops the broadcast; what was sent is in the stored counts.

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
| `TERAKOYA_ADMIN_IDS` | `admin_ids` | — | Telegram user IDs of the admins, comma-separated. They get `/emoji_setup`, `/broadcast` and `/nick_reset`. |
| `TERAKOYA_LOG_LEVEL` | `log_level` | `info` | `debug`, `info`, `warn` or `error`. |
| `TERAKOYA_RESVG` | `resvg` | `resvg` | The resvg binary that rasterizes SVG drawings. |
| `TERAKOYA_FONTS_DIR` | `fonts_dir` | `/usr/share/fonts/dejavu` or `/usr/share/fonts/truetype/dejavu` | DejaVu fonts for the drawings' labels; system fonts are not used, so drawings look the same everywhere. |
| `TERAKOYA_GIT_HOST` | `git_host` | — | `github` or `forgejo`. Enables error reports, git webhooks and sync; requires `TERAKOYA_REPO_URL`. |
| `TERAKOYA_GIT_API_URL` | `git_api_url` | `https://api.github.com` for GitHub | Base URL of the host. Required for Forgejo, e.g. `https://git.hyperchat.ru`. |
| `TERAKOYA_GIT_REPO` | `git_repo` | — | Content repository as `owner/name`. |
| `TERAKOYA_GIT_TOKEN` | `git_token` | — | Access token, used for the API and for git over HTTPS. See below for scopes. |
| `TERAKOYA_GIT_WEBHOOK_SECRET` | `git_webhook_secret` | — | Secret shared with the webhook; deliveries with a wrong signature are rejected. |
| `TERAKOYA_GIT_AUTHOR_NAME` | `git_author_name` | `terakoya` | Committer name of the commits that carry report pictures on GitHub. The author is the Telegram user. |
| `TERAKOYA_GIT_AUTHOR_EMAIL` | `git_author_email` | `terakoya@localhost` | Committer email. |
| `TERAKOYA_ACCEPTED_LABEL` | `accepted_label` | `accepted` | Label that marks an error report as accepted (see below). Created on start if missing. |
| `TERAKOYA_HTTP_ADDR` | `http_addr` | `:8080` | Address of the HTTP server for git webhooks (`POST /webhooks/git`, `GET /healthz`). |
| `TERAKOYA_SYNC_INTERVAL` | `sync_interval` | `5m` | How often to pull the content repository and check open reports, in case a webhook was missed. `0` disables. |
| `TERAKOYA_SUGGEST_COOLDOWN` | `suggest_cooldown` | `1m` | Minimum time between two reports of one user. |
| `TERAKOYA_SUGGEST_MAX_OPEN` | `suggest_max_open` | `5` | Maximum unresolved reports per user. |
| `TERAKOYA_IMAGE_MAX_BYTES` | `image_max_bytes` | `5242880` | Maximum size of a picture attached to a report. |
| `TERAKOYA_CONFIG` | — | — | Path to the YAML config file. |

### Settings file: ranks, custom emoji, topic list, top

What admins change while the bot runs lives in `settings.yml` in the data
directory (`./data/settings.yml` with Docker Compose, so it survives
redeploys). The bot creates it on first start with the default ranks
(Novice 0 · Pupil 1 · Student 3 · Scholar 7 · Mentor 15 · Teacher 30 ·
Master 60 · Sage 100 accepted reports), no custom emoji and the list
style for topic lists, and
rereads it within seconds when it is edited by hand. A file that does not
validate (the first rank must start at 0, thresholds must strictly
increase, emoji IDs are numbers, the topic list style is `list` or
`buttons`, `top.size` is 1 to 100, `top.medals` 0 to 5, `rank_style` one
of the four) is logged and ignored; the previous settings stay in use. A
file from before a section existed gets its defaults.

```yaml
topic_list_style: list   # or buttons
ranks:
  hide_future: true  # /ranks shows later ranks as «???» with their threshold
  ladder:
    - title: Novice
      from: 0
      emoji: ""      # custom emoji ID shown with the rank
    - title: Pupil
      from: 1
      emoji: "5368324170671202286"
top:
  size: 20            # number of places
  medals: 3           # first N places (0–5) show a custom emoji instead of the number
  rank_style: emoji   # none | title | emoji | title_emoji
  show_count: true
emoji:
  keyboard_subjects: ""
  keyboard_pinned: ""
  keyboard_account: ""
  keyboard_settings: ""
  pin: ""
  greeting: ""
  subjects:
    Геометрия: ""    # by the subject's folder name
  top: ["", "", ""]  # medals of the first places
```

`TERAKOYA_RANKS` is no longer a setting. If it is still set when the file
is created, its ranks seed the file once and the bot logs a warning;
after that it is ignored (with a warning) and can be removed.

Custom emoji need a bot owner with Telegram Premium. They show as icons on
buttons, in the greeting and rank-up messages, before the rank in
«Аккаунт», and as ranks and medals in the top; where none is set, there is no emoji at all. The bot checks
the IDs with Telegram when the settings load and does not use unknown
ones. If Telegram later refuses a message because of custom emoji (for
example, when the owner's Premium ends), the bot sends it without them,
logs it once and stays without custom emoji until the settings
change. Notes in the vault may use custom emoji too, as
`![👍](tg://emoji?id=…)`.

Admins set the emoji with **`/emoji_setup`**, a step-by-step dialog over
every place: the four bottom buttons, the pin button, the greeting, each
subject of the vault, each rank and the medals of the first `top.medals`
places («1 место», «2 место»…). Each step says where the emoji goes
and what it currently is; the admin replies with a custom emoji, confirms
the preview, or uses «Пропустить», «Без эмодзи» or «Отмена». The answers are
written to `settings.yml` at the end, with a summary; «Отмена» (or
`/cancel`) changes nothing and deletes the wizard's messages. `/emoji_setup <slot>` changes one place; the slots are
`keyboard_subjects`, `keyboard_pinned`, `keyboard_account`,
`keyboard_settings`, `pin`, `greeting`, `subject:<name>`, `rank:<title>`
and `top:1` … `top:5`, any of the five (an unknown slot lists them). Rank titles and thresholds are changed in the file for now.
Files from before `hide_future` existed, with `ranks:` as a plain list,
still load (with `hide_future: true`) and get the new layout the next
time the bot writes the file.

Outside `/emoji_setup`, an admin who sends the bot custom emoji gets each
one's ID back.

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

## Error reports

With `TERAKOYA_GIT_HOST` set, users can report errors without knowing
anything about Markdown, LaTeX, files or git. The bot never shows them
file names, frontmatter or syntax; everything is buttons and plain text.
Reports are open to everyone, within the cooldown and the cap on open
reports (`TERAKOYA_SUGGEST_COOLDOWN`, `TERAKOYA_SUGGEST_MAX_OPEN`).

- Under every note, **«Сообщить об ошибке»** asks right away what is
  wrong. The user describes it in their own words, in one message or
  several, optionally with photos or screenshots, and taps «Отправить».
- Under a grade's topic list, **«Сообщить об ошибке»** is for something
  missing in the grade or one of its topics («нет темы про подобные
  треугольники»). It works the same way.

Each report becomes an **issue** for a maintainer to fix by hand: the
user's text, who sent it (name, @username, Telegram ID), a link to the
note, or for a grade to its folder, at the commit the user saw and on the
default branch, and the pictures. On Forgejo pictures are issue
attachments; GitHub has no API for those, so they are pushed to a
`report/<tg_id>-<slug>-<short_id>` branch (into the topic's or grade's
`_assets/` folder) and linked from the issue. Issues about a note are
titled «Error report: <note>», those about a grade «Missing material:
Геометрия · 7 класс».

Every step has «Отмена», and `/cancel` does the same. It says nothing:
every message of the dialog goes, the bot's questions as well as the
user's texts and photos (deleted in the background), and the user is back
where the dialog started: the note, sent again in place of the old copy,
or the topic list. If that note has left the vault meanwhile, they get
its topic list, or failing that the subject list.

A grade's topic list is, by default (`topic_list_style: list`), a rich
message: the heading «Геометрия · 10 класс» (with the subject's custom
emoji, if set) and one line per topic, «§5 Параллельность прямой и
плоскости», the number taken from the folder prefix without leading zeros
(topics without a number have just the title). Each line is an in-text
button that opens the topic in place of the list. Under it are only
«Сообщить об ошибке» (when reports are on) and «Назад». With
`topic_list_style: buttons` the topics are inline buttons instead, with
the same «§5 » labels.

Outcomes:

- An issue closed with the `accepted` label (`TERAKOYA_ACCEPTED_LABEL`)
  counts toward the user's rank, and they get a thank-you message with
  «Принято правок: N», a rank-up line when they reach a new rank, and,
  for a note, a button to open it.
- An issue closed without the label sends the user the last review
  comment. The bot waits two minutes after the close, so closing first and
  labeling right after is fine. Adding the label to an issue that was
  already reported as not accepted still counts it, once.
- Every `TERAKOYA_SYNC_INTERVAL`, the bot also asks the host about each open
  report, so outcomes are noticed even when a webhook delivery is lost.

The bot no longer edits notes or opens pull requests; edit those in the
vault yourself. Pull requests it opened in earlier versions are still
followed: their authors are told when they are merged or closed, without
counting toward the rank. On notes and topic lists those versions sent,
«Предложить правку» and «Добавить тему» now start a report; their other
buttons still on users' screens, such as «Исправить текст», lose their
keyboard when tapped.

The checkout in `TERAKOYA_REPO_DIR` belongs to the bot: syncing resets it
to the remote branch. The bot refuses to use a checkout whose `origin` is
not `TERAKOYA_REPO_URL`. Use an HTTPS `TERAKOYA_REPO_URL`: links in
issues are built from it.

### GitHub

- Token: a fine-grained personal access token for the content repository
  with **Contents** and **Issues: read and write** (contents for the
  pictures' `report/…` branches), and **Pull requests: read** for pull
  requests opened by earlier versions.
- Webhook (repository → Settings → Webhooks): payload URL
  `https://<your host>/webhooks/git`, content type `application/json`,
  secret = `TERAKOYA_GIT_WEBHOOK_SECRET`, events **Pushes** and
  **Issues** (and **Pull requests** while pull requests from earlier
  versions are open).

### Forgejo

- Token (user settings → Applications): scopes **repository: read and
  write** and **issue: read and write** (error reports, their pictures and
  the label), for a user with write access to the content repository.
- Webhook (repository → Settings → Webhooks → Forgejo): target URL
  `https://<your host>/webhooks/git`, content type `application/json`,
  secret = `TERAKOYA_GIT_WEBHOOK_SECRET`, custom events **Push**,
  **Issues** and **Issue label** (and **Pull request** while pull requests
  from earlier versions are open).

The webhook server listens on `TERAKOYA_HTTP_ADDR`; put it behind a
reverse proxy with TLS. Without working webhooks, everything still
happens, only up to `TERAKOYA_SYNC_INTERVAL` later: the content syncs and
open reports are checked on that schedule. A label added to an
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
