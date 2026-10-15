# Content guide

The catalog is a directory of plain Markdown files. It reads well in
Obsidian, on GitHub and in any Markdown editor; the bot only renders it.
Run `terakoya lint <content-dir>` before committing; CI in the content
repository should run it too.

## Layout

```
content/
  geometry/
    _subject.yml          # subject: title, emoji, order
    7/
      _grade.yml          # optional: title, notes, order
      01-basic-notions.md
      02-triangles.md
      assets/
        triangle.png
```

- A **subject** is a directory with a `_subject.yml`. Directories without
  one are ignored (the linter warns).
- A **grade** is any directory inside a subject. Its articles are the `.md`
  files directly inside it. The only nested directory allowed is `assets`.
- Names starting with `.` or `_` are ignored, so `.obsidian/` and
  `_drafts/` are safe.
- Subject and grade directory names together must not exceed 56 bytes.
  Short ASCII names such as `geometry` and `7` are best.

### `_subject.yml`

```yaml
title: Геометрия   # required
emoji: 📐          # optional, shown before the title
order: 1           # optional, subjects are sorted by order, then name
```

### `_grade.yml`

```yaml
title: 7 класс              # optional, defaults to "<dir> класс"
notes: Начала планиметрии.  # optional, shown above the topic list
order: 7                    # optional, defaults to the directory name if numeric
```

## Articles

Every article starts with YAML frontmatter:

```markdown
---
title: Треугольники
order: 2
id: triangles
aliases:
  - Треугольник
---

# Треугольники
…
```

| Key       | Required | Meaning |
|-----------|----------|---------|
| `title`   | yes      | Shown in the topic list and used by `[[links]]`. |
| `order`   | no       | Position in the topic list. Defaults to the numeric file name prefix (`02-…` → 2). |
| `id`      | no       | Stable article ID, see below. |
| `aliases` | no       | Extra names that `[[links]]` may use. |

`tags` and `cssclasses` are accepted for Obsidian; other keys produce a
warning.

### Article IDs

Deep links (`https://t.me/<bot>?start=<id>`) and buttons use the article ID.
By default it is derived from the path: `<subject>-<grade>-<slug>`, where the
slug is the file name without `.md` and without the numeric prefix. So
`geometry/7/01-basic-notions.md` has the ID `geometry-7-basic-notions`.

Set `id` in the frontmatter to keep the ID stable when a file is renamed or
moved; old deep links and `[[links]]` keep working. Set it as well when the
file name is not ASCII.

IDs are 1–56 characters of `A–Z a–z 0–9 _ -` and must be unique across the
whole catalog.

## Markdown

Articles use GitHub-flavoured Markdown: headings, emphasis, lists, task
lists, tables (up to 20 columns), block quotes, code blocks and footnotes.

### Math

- Inline math: `$a^2 + b^2 = c^2$`. No space right after the opening `$` or
  right before the closing one.
- Block math: `$$ … $$`, either on one line or with the delimiters on their
  own lines. Obsidian renders both forms.

  ```markdown
  $$
  \angle A + \angle B + \angle C = 180^\circ
  $$
  ```

  A blank line inside a `$$` block ends it, so keep formulas compact
  (use `aligned` for multi-line formulas). Nothing may follow the closing
  `$$` on the same line.
- A literal dollar sign is written as `\$`: `тетрадь стоит \$2`.
  An unescaped `$` that does not start a formula produces a warning.
- Math inside code spans and code blocks is left alone.

### Images

- Use standard Markdown with a path relative to the article:
  `![Треугольник ABC](assets/triangle.png)`.
- An optional title becomes the caption:
  `![Треугольник ABC](assets/triangle.png "Рис. 1")`.
- **Each image must sit on its own line**, separated from text by blank
  lines. Telegram shows media only as separate blocks; an image inside a
  paragraph is an error and is replaced by its alt text.
- Supported formats: PNG, JPEG, WebP; at most 10 MB each. SVG is not
  supported; export it to PNG.
- Images must stay inside the content directory.
- Obsidian embeds (`![[picture.png]]`) are not supported. In Obsidian, turn
  off *Settings → Files and links → Use [[Wikilinks]]* so new images are
  inserted as standard Markdown.

### Links between articles

- `[[Треугольники]]` links by title or alias (case-insensitive).
- `[[triangles]]` links by ID; `[[basic-notions]]` links by file slug.
- `[[Треугольники|текст ссылки]]` sets the link text.
- `[[Треугольники#Сумма углов]]` is accepted; the heading part is ignored.
- Inside tables, write the separator as `\|`: `[[Треугольники\|треугольник]]`.

If the same title appears in several grades, a link resolves to the
article in the linking article's grade. Use the ID to link elsewhere.
Unresolved links are errors.

Regular links to websites (`[text](https://…)`) work as usual.

## Limits

An article is sent as one Telegram message, which allows at most:

| Limit | Value |
|-------|-------|
| Text characters (including formula source) | 32768 |
| Blocks (paragraphs, list items, table rows, quotes, …) | 500 |
| Images | 50 |
| Nesting depth of blocks and formatting | 16 |
| Table columns | 20 |

The linter measures these conservatively. Split articles that come close.
