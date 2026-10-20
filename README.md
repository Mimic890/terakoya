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
- [x] Full-text search with stemming, typos, filters and a list of missing material for admins
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
    # The bot needs about 30 MB; the cap keeps a runaway SVG conversion
    # from taking the server's memory.
    mem_limit: 512m
    env_file: .env
    environment:
      # The time zone of the daily backup time in settings.yml.
      TZ: Europe/Minsk
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

### Shutdown and limits

On `SIGTERM` (`docker compose down`, a deploy) the bot stops taking
updates and waits up to 8 seconds, less than Docker's 10, for work it
left running in the background: deleting old messages, recording
activity, a broadcast (which pauses and resumes on the next start, see
[Broadcasts](#broadcasts)). A git command that is cut off gets
`SIGTERM` and 5 seconds to clean up; a lock file a killed one left in the
vault checkout is removed before the next sync. A panic while handling
an update, in background work, a sync or a webhook is logged with its
stack, and the bot goes on.

Each user may send a burst of 20 updates (an album of pictures with a
caption fits) and then 3 a second; beyond that, taps and messages are
dropped, so one user cannot use up Telegram's limit for the whole bot.
Admins are not limited. A user who blocked the bot is noticed whenever a
message to them fails with 403, and broadcasts skip them until they come
back.

Once a day the bot removes rows it has no use for: the message groups of
notes sent more than 7 days ago (the bot can no longer delete such
messages anyway), dialogs that expired over a day ago, and note opens
and search queries older than 180 days.

`/admin` shows the version the binary was built with: the commit for
images built by CI (the `REVISION` build argument), otherwise `dev`. Set
it by hand with
`-ldflags "-X github.com/Mimic890/terakoya/internal/bot.Version=…"`.

## Commands

```
terakoya [serve] [-config file]     run the bot (default)
terakoya lint [-strict] <vault>     validate a vault
terakoya admin [--json] <command>   talk to the running bot (see below)
```

In Telegram, `/start` sends a greeting («Привет, <name>! Это terakoya —
школьный справочник: теоремы, аксиомы, задачи и доказательства по
классам. Нашли ошибку или чего-то не хватает? Сообщите — за принятые
правки растёт ранг.») that sets the keyboard at the
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
rejected ones; titles of notes in the vault open them. Accepted reports
count toward the rank. «Топ» there, or `/top`, shows the
[leaderboard](#top), and «Ранги», or `/ranks`, [the ladder](#ranks). These pages are rich messages, split like long notes. A tap on
the keyboard shows the new menu first; the tap itself and the previous
menu are deleted afterwards, in the background, so the chat does not fill
up. Tapping the button of the menu that is already the last message only
deletes the tap. Typed commands are deleted once answered. Every note has
«Закрепить» / «Открепить» (at most 30 pins); a note opened from the
pinned list goes back to it with «Назад». Pins of notes removed from the
vault disappear from the list. `/download` sends the whole vault as a zip
(without hidden files such as `.obsidian`). Any text outside a dialog is a
[search](#search).

The interface has no emoji of its own.

Admins also have [`/admin`](#admin-panel), `/emoji_setup` (see
[the settings file](#settings-file-ranks-custom-emoji-topic-list-top)),
[`/broadcast`](#broadcasts) and `/nick_reset <id or @username>`. For
everyone else these commands do nothing. The command menu shows
`/start`, `/search`, `/download`, `/top`, `/ranks` and `/cancel` to everyone, and the admin
commands only in the admins' chats (set on start; an admin who has never
opened the bot gets them after the next restart).

### Search

Any text message outside a dialog is a search query, and so is
`/search <query>`; `/search` alone asks «Что найти? Напишите слово или
фразу, например «теорема Пифагора».» and takes the next message. Text
typed inside a dialog (a report, a nickname, a broadcast) belongs to the
dialog and is never searched; the rate limit applies as to everything
else. The query is deleted once the results are shown, and the results
replace the current menu.

The index is built in memory each time the vault is loaded (on start and
after every sync; about 0.4 s for 2000 notes). Each note contributes its
title, headings, topic, subject and kind, and its text without Markdown:
pictures go, links keep their text, tables their cells, and LaTeX reads
as words (`\parallel` as «параллельна», `\perp` «перпендикулярна»,
`\angle` «угол», `\triangle` «треугольник», Greek letters by name), so
«параллельны» finds `$a \parallel b$`. Words are lowercased, ё is read as
е, Russian and English words are stemmed (Snowball) and stop words
dropped. Ranking is BM25 with field weights (title ≫ headings > topic >
text), boosted when all words are present and again when they come as a
phrase; on equal scores a topic's main page comes first. The last word
also matches as the start of a word («параллел» finds «параллельность»).
Words that match nothing, or all words when fewer than 3 notes are found,
also match with typos: one for words of 4–7 letters, two from 8.

Filters in the query narrow the results and are not searched for: a grade
(«7 класс», «10 кл», «10-й класс») and a kind («теорема», «задача»,
«аксиома», «следствие»; a note whose title has the word counts, such as a
main page «Теорема Пифагора»). A query of filters alone lists everything
they let through.

«Поиск: «запрос»» shows «Найдено: N», 8 results per page with ‹ and ›:
each is «Теорема · title», a link that opens the note, «Геометрия · 10
класс · §5 topic» and about 120 characters of text around the words
found, in bold. «Назад» under a note opened from the results returns to
them. With no results, «Ничего не нашлось.» offers «Возможно, вы имели в
виду:» other queries as buttons (the closest words of the index, or the
query without one of its filters) and, when reports are on, «Сообщить,
что не хватает материала»: a report about the subject (asked first if
there are several) that already says «По запросу «…» ничего не нашлось.»
and can be sent as it is or with more text and pictures. Each chat's last
search lives in memory; after a restart its buttons ask for the query
again.

Queries are stored normalized, with how many notes they found and when,
never with who searched, for [/admin](#admin-panel), and removed after
180 days.

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
ник.»). Saving or «Отмена» returns to the «Ник» screen. The name may
change once a day: the first nickname is free, and after any change,
to a nickname or back to the Telegram name, both buttons answer «Имя
можно менять раз в сутки. В следующий раз — через 23 ч 5 мин.» until a
day has passed. After an admin's `/nick_reset` a new one may be chosen
right away. A user who hides from the top
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
One broadcast is sent at a time; «Да, отправить» while another is
running says so and keeps the dialog, to be pressed again later.

Broadcasts, their counts and every recipient's state are stored in the
database. On shutdown a broadcast stops between two recipients and
resumes on the next start, with the same progress message. Each
recipient is marked before the message goes out, so even if the process
dies at that moment nobody gets a second copy: that one recipient counts
as not delivered instead.

### Admin panel

`/admin` is a rich message, «Admin panel», with tables and no emoji:

- **Сервер**: CPU use of the process over one second (percent of one
  core, and the number of cores), memory (RSS, Go heap, goroutines),
  free and total space of the data directory's disk, the container's
  network traffic since it started, the Telegram API traffic (calls,
  bytes sent and received) today and in total, uptime and the version.
- **Хранилище**: disk use of the vault (notes, pictures, .git), the data
  directory, the database and the cached file_ids; the backup mode, the
  last backup and the next one. Directory sizes are reused for a minute.
- **Vault**: the commit and its age, notes, and the last sync with its
  outcome.
- **Пользователи**: all, new today and in 7 days, active in 24 hours and
  7 days, blocked the bot.
- **Сообщения об ошибках**: open, accepted and rejected, in total and in
  the last 7 days.
- **Документы**: note opens today, in 7 and in 180 days (older counts are
  removed), and the 10 most opened notes in 7 and in 180 days by title
  and folder. A note gone from the vault keeps the title it had when
  last opened; opens counted before titles were kept are left out.
- **Поиск**: the 10 queries searched for most in 7 days, and the 10
  searched for most in 180 days whose last search found nothing: the
  material users miss.

Under it:

- **Пользователи**: «Users», a table of 25 users a page, most recently
  active first: ID, @username, first name, nickname, rank, accepted,
  open and rejected reports, first and last seen, the place in /top as
  everyone sees it («скрыт» for one who hides, «нет» without accepted
  reports or below the top), blocked the bot. «‹ Назад» and «Вперёд ›» appear only when
  there is a page to go to; «В админку» goes back.
- **Обновить vault** syncs now, as a push webhook does: «Обновление
  vault…» while it runs, then the panel anew and the outcome under it.
  A clean outcome is removed after a second; an error or lint findings
  (the old and new commit, notes before and after, the first five
  errors and warnings) stay.
- **Обновить** measures again.

Rich messages cannot be edited, so every screen is sent anew and the
previous one removed. `/broadcast` and `/nick_reset` are separate admin
commands, not part of the panel.

Measuring and syncing run in the background, with a time limit. To keep
these numbers, the bot records when each user was last seen (at most
every 5 minutes per user), how often each note is opened per day, the
search queries (see [Search](#search)), and the bytes of its Telegram API
calls per day.

### Admin CLI

The running bot also answers on a unix socket, `admin.sock` in the data
directory, readable only by the user the bot runs as. There is no token:
whoever can open the socket file is an admin. `terakoya admin` talks to
it; in Docker, run it in the bot's container:

```sh
docker compose exec terakoya terakoya admin status
docker compose exec terakoya terakoya admin server
docker compose exec terakoya terakoya admin users --search аня --page 2
docker compose exec terakoya terakoya admin user @anya
docker compose exec terakoya terakoya admin nick-reset 123456789
docker compose exec terakoya terakoya admin sync
docker compose exec terakoya terakoya admin backup
docker compose exec -it terakoya terakoya admin broadcast --text "Завтра обновление"
```

Commands: `status`, `server`, `stats`, `users [--search q] [--page n]`,
`user <id|@username>`, `nick-reset <id|@username>`, `sync`, `backup`
(see [Backups](#backups)) and `broadcast --text "…" [--photo path] [--yes]`. The output is plain text;
`--json` prints JSON for scripts. Errors exit with status 1. `broadcast`
prints the number of recipients and asks `[y/N]` first (`-it` gives it a
terminal; `--yes` skips the question). Its text is sent as is, without
formatting; a `--photo` (a path inside the container) is uploaded once,
to the cache chat or the first admin's chat (and deleted there), and
sent with it. The message first goes to the first admin as a test copy;
if Telegram refuses it, nothing is sent to anyone and the command fails.
The broadcast then runs in the background like one from `/broadcast`;
while another one runs, the command fails and sends nothing.
`--socket path` (or `TERAKOYA_ADMIN_SOCKET`) points to another socket.

### Backups

The bot backs up its data, the SQLite database and `settings.yml`, into
`data/backups/` (`/srv/data/backups` in the container). Copying them
somewhere else is up to you, e.g. with `rsync` or `restic` from the host.
The `backup:` section of `settings.yml` sets it:

```yaml
backup:
  mode: folder   # folder | archive | off
  time: "03:00"  # daily, HH:MM in the container's time zone (TZ)
  keep: 7        # archive mode only: how many archives to keep, 1–365
```

- **`folder`** (the default): every day at `time` the bot writes
  `data/backups/latest/` with `terakoya.db` and `settings.yml`, and keeps
  nothing else, for a tool on the host to archive. The new folder is
  built as `data/backups/.tmp-…/` and swapped in only when complete:
  `latest/` is always a whole backup.
- **`archive`**: every day at `time` the bot writes
  `data/backups/terakoya-2026-10-20-0300.tar.gz` with the same two files,
  under a temporary name renamed when complete, and keeps the newest
  `keep` archives.
- **`off`**: no scheduled backups.

In every mode the database is copied with `VACUUM INTO` while the bot
runs and checked with `PRAGMA quick_check` before it is used; the copies
have the permissions of the originals (the settings are readable by
their owner only). `time` is in the container's local time: set `TZ`
in the compose file (see [Docker](#docker)); without it, it is UTC.

- If the bot was down at `time` and the last backup is more than a day
  old when it starts, it takes one right away.
- `terakoya admin backup` takes one now, in the current mode (the folder
  mode when backups are off), e.g. before a risky change.
- Before the database schema changes on an upgrade, the bot copies the
  database and `settings.yml` to `data/backups/before-v<N>/`, whatever the
  mode; the 3 newest of these are kept.
- A change of the `backup:` section, by hand or otherwise, takes effect
  within seconds, without a restart.

/admin «Хранилище», `terakoya admin server` and `terakoya admin backup`
show the mode, the last backup with its size (or its error) and when the
next one is due. A failed backup is also logged.

`TERAKOYA_BACKUP_INTERVAL` and `TERAKOYA_BACKUP_KEEP` of earlier versions
are ignored, with a warning in the log.

To restore a backup:

```sh
docker compose stop terakoya
cp data/backups/latest/terakoya.db data/terakoya.db   # or unpack an archive
rm -f data/terakoya.db-wal data/terakoya.db-shm
cp data/backups/latest/settings.yml data/settings.yml  # if wanted
docker compose start terakoya
```

Removing the `-wal` and `-shm` files matters: they belong to the old
database and must not be applied to the restored one. Keep the files'
owner (`chown 10001:10001` or whatever `user:` is in the compose file).
A backup from an older version of the bot is fine; it is migrated on
start.

`lint` prints one `path:line: severity: message` line per problem and exits
with status 1 if there are errors (or warnings, with `-strict`). It checks
the folder structure, links and images (broken or ambiguous), duplicate
topic numbers, notes without a title, unexpected files, symbolic links,
SVGs that fail to rasterize, math delimiters and Telegram message limits, and lists notes
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

### Settings file: ranks, custom emoji, list style, top

What admins change while the bot runs lives in `settings.yml` in the data
directory (`./data/settings.yml` with Docker Compose, so it survives
redeploys). The bot creates it on first start with the default ranks
(Novice 0 · Pupil 1 · Student 3 · Scholar 7 · Mentor 15 · Teacher 30 ·
Master 60 · Sage 100 accepted reports), no custom emoji and the list
style for subjects, grades and topics, and
rereads it within seconds when it is edited by hand. A file that does not
validate (the first rank must start at 0, thresholds must strictly
increase, emoji IDs are numbers, the list style is `list` or
`buttons`, `top.size` is 1 to 100, `top.medals` 0 to 5, `rank_style` one
of the four) is logged and ignored; the previous settings stay in use. A
file from before a section existed gets its defaults. The list style was
called `topic_list_style` when it applied to topics only; a file with the
old key is read as before and rewritten with `list_style` on start.

```yaml
list_style: list   # or buttons: subjects, grades and topics
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
twice their size, at most 4096 px on the longer side, on a white
background, only when their PNG is not cached yet: the cache key is the
hash of the SVG source plus the rendering settings, so an unchanged
drawing is never converted or uploaded again. Users opening a new note at
the same time share one conversion and one upload. If a drawing cannot be
converted, the note is sent without it and the error is logged (lint
reports it too).

Before resvg runs, the bot checks the drawing: at most 5 MB and
4096 × 4096 px (from `width`, `height` or `viewBox`), and every picture
it embeds must be a `data:` URI or a file inside the vault, by a relative
path without symbolic links. resvg would otherwise read any file on the
server it is pointed at.

The bot never follows symbolic links in the vault: its checkout stores
them as plain files (`core.symlinks=false`), the loader, the `/download`
archive and image uploads skip them, and `lint` reports each as an
error. A link could otherwise point at a file on the server, such as
the one holding the bot's tokens.

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
- Under a subject's grade list, **«Сообщить об ошибке»** is for the
  subject as a whole («нет 11 класса»). The issue is titled «Missing
  material: Геометрия» and links the subject's folder; «Отмена» returns
  to the grade list.

A report's text, all its messages together, may be up to 4000
characters; a message that would go over is not added, and the user is
told. If sending fails halfway, say an attachment upload, the dialog
stays and «Отправить» goes on from there: the issue already opened and
the pictures already attached are reused, never opened or uploaded
twice.

Each report becomes an **issue** for a maintainer to fix by hand: the
user's text, who sent it (name, @username, Telegram ID), a link to the
note, or for a grade to its folder, at the commit the user saw and on the
default branch, and the pictures. On Forgejo pictures are issue
attachments; GitHub has no API for those, so they are pushed to a
`report/<tg_id>-<slug>-<short_id>` branch (into the topic's or grade's
`_assets/` folder) and linked from the issue. Issues about a note are
titled «Error report: <note>», those about a grade «Missing material:
Геометрия · 7 класс».

Whatever the user wrote, their names and picture captions appear in the
issue as code, verbatim: as Markdown they could mention people on the
host (`@user`), link other issues (`#12`) or load images from elsewhere
in the reviewer's browser. Pictures must really be PNG or JPEG: the bot
checks the bytes, not the name or type the app sent, before it opens the
issue, and a picture that is neither is taken out of the report with a
note to the user.

Every step has «Отмена», and `/cancel` does the same. It says nothing:
every message of the dialog goes, the bot's questions as well as the
user's texts and photos (deleted in the background), and the user is back
where the dialog started: the note, sent again in place of the old copy,
or the topic list. If that note has left the vault meanwhile, they get
its topic list, or failing that the subject list.

Subjects, grades and topics are listed the same way, set by
`list_style` in `settings.yml`. By default (`list`) each list is a rich
message, a heading and one line per item; each line is an in-text button
that opens the item in place of the list. A subject's custom emoji, if
set, goes in front of its line and in the headings of its grade and
topic lists; without one there is nothing, not even a space.

- **Subjects:** «Предметы», then «Геометрия · 2 класса» per subject,
  with the number of grades. Under it only «Закрыть»: there is nothing to
  go back to.
- **Grades:** «Геометрия», then «10 класс · 5 тем» per grade, with the
  number of topics. Under it «Сообщить об ошибке» (when reports are on)
  and «« Назад» in one row.
- **Topics:** «Геометрия · 10 класс», then «§5 Параллельность прямой и
  плоскости» per topic, the number taken from the folder prefix without
  leading zeros (topics without a number have just the title). Under it
  «Сообщить об ошибке» (when reports are on) and «Назад».

Moving between the levels replaces the list with the next one, and
«Назад» brings the previous one back; a rich list is always sent anew,
never edited into text. With `list_style: buttons` all three are text
menus with an inline button per item, edited in place, the subject's
emoji on its button, and the same buttons underneath.

Outcomes:

- An issue closed with the `accepted` label (`TERAKOYA_ACCEPTED_LABEL`)
  counts toward the user's rank, and they get a thank-you message with
  «Принято правок: N», a rank-up line when they reach a new rank, and,
  for a note, a button to open it.
- An issue closed without the label sends the user the last review
  comment, cut to 1000 characters. The bot waits two minutes after the close, so closing first and
  labeling right after is fine. Adding the label to an issue that was
  already reported as not accepted still counts it, once.
- A delivery only tells the bot that an issue changed; it reads the
  issue's state and labels from the host's API, so a replayed or late
  delivery cannot change an outcome.
- Every `TERAKOYA_SYNC_INTERVAL`, the bot also asks the host about each open
  report, so outcomes are noticed even when a webhook delivery is lost.

The bot does not edit notes or open pull requests; fixes are made in the
vault by hand. On notes and topic lists sent by early versions,
«Предложить правку» and «Добавить тему» start a report; their other
buttons still on users' screens, such as «Исправить текст», lose their
keyboard when tapped.

The checkout in `TERAKOYA_REPO_DIR` belongs to the bot: syncing resets it
to the remote branch. The bot refuses to use a checkout whose `origin` is
not `TERAKOYA_REPO_URL`. Use an HTTPS `TERAKOYA_REPO_URL`: links in
issues are built from it.

### GitHub

- Token: a fine-grained personal access token for the content repository
  with **Contents** and **Issues: read and write** (contents for the
  pictures' `report/…` branches).
- Webhook (repository → Settings → Webhooks): payload URL
  `https://<your host>/webhooks/git`, content type `application/json`,
  secret = `TERAKOYA_GIT_WEBHOOK_SECRET`, events **Pushes** and
  **Issues**.

### Forgejo

- Token (user settings → Applications): scopes **repository: read and
  write** and **issue: read and write** (error reports, their pictures and
  the label), for a user with write access to the content repository.
- Webhook (repository → Settings → Webhooks → Forgejo): target URL
  `https://<your host>/webhooks/git`, content type `application/json`,
  secret = `TERAKOYA_GIT_WEBHOOK_SECRET`, custom events **Push**,
  **Issues** and **Issue label**.

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
The search tests time indexing 2000 notes (under 1 s) and queries (under
20 ms); they are skipped with `-race` and `-short`. The SVG test runs resvg when it is installed (`TERAKOYA_RESVG` and
`TERAKOYA_FONTS_DIR` point to it) and is skipped otherwise.
