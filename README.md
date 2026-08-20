# wikit CLI

[![English](https://img.shields.io/badge/Lang-English-2563eb?style=for-the-badge)](README.md)&nbsp;[![中文](https://img.shields.io/badge/语言-中文-9ca3af?style=for-the-badge)](README.zh-CN.md)

A cross-platform (Windows / Linux / macOS) Wikidot farm-wiki archiving tool, powered by Go. It
produces backups that are **compatible with the WikiComma archive format** — the
same directory layout and byte-for-byte the same file contents. The backup can be directly imported for use with the [ProjectWikit Engine](https://github.com/WikitTeam/ProjectWikit).


## What gets archived

`wikit` captures essentially everything a Wikidot wiki exposes:

### Wiki metadata (`meta/site.json`)
- Domain
- Global site ID
- Slug (lowercase unix name)
- Home page
- Language

### Page metadata (`meta/pages/<name>.json`)
- Global page ID
- Name
- Title
- Rating
- Tags
- Parent page
- Forum discussion thread ID
- Lock status
- Last-updated timestamp
- Full revision list
- Votes and voters (per-user up/down)
- Attached files list

### File metadata & contents
- Global file ID
- Name
- Original URL
- Size (human-readable and in bytes)
- MIME type and reported content type
- Author (numeric ID)
- Upload timestamp
- The **raw file bytes** (`files/<page>/<file_id>`)

### Page revisions
- Global revision ID
- Per-page revision number
- Timestamp
- Author (numeric ID)
- Change flags
- Commentary
- The **full wiki source text** of every revision (compressed in
  `pages/<name>.7z`)

### Forum — categories (`meta/forum/category/<id>.json`)
- Title, description, global ID
- Time of last post
- Total posts and threads
- Last poster (numeric ID)

### Forum — threads (`meta/forum/<cat>/<thread>.json`)
- Global ID, title, description
- Created when / by whom
- Last post time / last poster
- Post count, sticky flag, lock status
- Full nested post tree

### Forum — posts
- Global ID, title, author, timestamp
- Last edit time / last editor
- Every post revision's **HTML content** (compressed in
  `forum/<cat>/<thread>.7z`)
- Nested replies (recursive tree)

### Users (`_users/<bucket>.json`)
- Display name and username slug
- Account creation date
- Account type (e.g. Pro)
- Activity / karma level
- (Users are bucketed by `id >> 13`.)

## Install

**Linux / macOS**
```
curl -fsSL https://raw.githubusercontent.com/kakushi-w/wikit/main/install.sh | sh
```

**Windows (PowerShell)**
```
irm https://raw.githubusercontent.com/kakushi-w/wikit/main/install.ps1 | iex
```

Open a new terminal, then run `wikit`.

Prefer to do it by hand? Download a binary from the
[Releases](https://github.com/kakushi-w/wikit/releases) page and run
`wikit install` once.

## Usage

```
wikit backup all                 # back up every wiki in config.json
wikit backup <name> [name...]    # back up specific wikis, Separate multiple wikis with spaces
                                 # a name not in the config is fetched from
                                 # https://<name>.wikidot.com
wikit fixpage <name> [name...]   # repair archives written by older wikit builds
                                 # (see "Repairing archives from older builds")
```

### Flags (override config.json values)

```
-c, --config <path>      config file (default ./config.json or $WIKIT_CONFIG)
    --base-dir <path>    override base_directory
    --bucket-size <n>    ratelimit bucket size
    --refill-seconds <n> ratelimit refill seconds
    --delay-ms <n>       delay between jobs (ms)
    --max-jobs <n>       maximum simultaneous wikis
    --user-cache <n>     user-info cache freshness (seconds)
    --http-proxy <s>     http proxy: host:port or host:port:user:password
    --socks-proxy <s>    socks proxy: host:port
    --no-update-check    do not check for a newer wikit release
    --refresh-votes      after backup, bulk-refresh page ratings/votes
    --scheme <s>         default scheme for wikis whose url omits one (default https)
    --keep-removed       keep pages that disappeared from the sitemap
    --checkpoint-pages <n>   pages between resume checkpoints (default 50)
    --checkpoint-seconds <n> seconds between resume checkpoints (default 30)
```

### Repairing archives from older builds

Run this once from the directory you normally back up from:

```
wikit fixpage <name> [name...]   # repair one or more wikis
wikit fixpage all                # repair every wiki in config.json
```

It rebuilds both `pages/*.7z` and `forum/**/*.7z`, resolving the archive
location exactly as a backup would (`base_directory`, overridable with
`--base-dir`). Archives already in the correct layout are left untouched, so
the command is safe to re-run; each archive is rebuilt in a temporary
directory and swapped in only once complete, and file contents are preserved
byte for byte.

```
-c, --config <path>      config file (default ./config.json or $WIKIT_CONFIG)
    --base-dir <path>    override base_directory
    --dry-run            list the archives that need repair without writing
```

### Resuming an interrupted backup

A backup records its progress in `meta/sitemap.json` as it goes, so a run that is
killed part-way (Ctrl+C, a crash, a lost connection) picks up where it stopped
instead of re-downloading every page's revisions from scratch on the next run.

A checkpoint is written once **both** thresholds are crossed — by default 50
newly-archived pages *and* 30 seconds since the last one. This flag affects only page versions that have already been converted to 7z files. Page folders that have not yet been converted are unaffected by interruptions and will automatically resume from where they left off.

```
wikit backup scp-wiki --checkpoint-pages 10 --checkpoint-seconds 5   # checkpoint more often
wikit backup scp-wiki --checkpoint-pages 0                           # time-based only
wikit backup scp-wiki --checkpoint-seconds -1                        # never checkpoint
```

- `0` drops that condition, leaving the other one in charge.
- A **negative** value on either setting disables checkpointing entirely, so the
  sitemap is only written when the run finishes.

Lowering both values costs more disk writes but loses less work to an interrupt;
raising them does the opposite. A completed run always writes the same final
sitemap regardless of these settings.

### Refreshing ratings and votes

The normal scan can't see rating/vote changes (they don't bump a page's
timestamp), so stored ratings go stale. `--refresh-votes` updates them after the
backup, rewriting only page metadata:

```
wikit backup scp-wiki --refresh-votes
```

## Updating

```
wikit update            # download and install the latest release
wikit update --check    # only report whether a newer version exists
wikit version           # print the installed version
```

After each `backup` run, wikit also does a cached (once-a-day) check and prints a
one-line notice if a newer version is available — disable with
`--no-update-check` or `WIKIT_NO_UPDATE_CHECK=1`.

## Config

`config.json` format:

```json
{
  "base_directory": "/data",
  "wikis": [ { "name": "scp-wiki", "url": "https://scp-wiki.wikidot.com" } ],
  "ratelimit": { "bucket_size": 60, "refill_seconds": 60 },
  "delay_ms": 200,
  "user_list_cache_freshness": 86400,
  "http_proxy": null,
  "socks_proxy": null,
  "refresh_votes": false,
  "scheme": "https",
  "keep_removed": false,
  "checkpoint_pages": 50,
  "checkpoint_seconds": 30
}
```

`refresh_votes`, `scheme`, `keep_removed`, `checkpoint_pages` and
`checkpoint_seconds` can be set here or overridden per-run with the matching
flags (the flag wins). `keep_removed` keeps pages deleted from the wiki instead
of removing them from the local archive; the two `checkpoint_*` keys tune how
often an interrupted run's progress is saved (see
[Resuming an interrupted backup](#resuming-an-interrupted-backup)).

Each wiki's `url` is optional (omit it to derive `https://<name>.wikidot.com`)
and sets that wiki's protocol: a `url` written with `http://` or `https://` is
used as-is, so wikis can mix protocols. `scheme` is only the default for wikis
whose `url` omits one (and for bare command-line names); set it to `http` when
your default wiki is HTTP-only.

```json
"wikis": [
  { "name": "a" },                              // https://a.wikidot.com
  { "name": "b", "url": "http://b.example.com" } // http, custom domain
]
```

A config file is only required for `backup all`, which reads the wiki list from
it. When you name wikis explicitly (`wikit backup <name> ...`) and there is no
`config.json`, wikit runs with these same defaults built in — archiving into a
`wikit_data` folder created in the current working directory (where you launched
the command). Use `--base-dir` (and the other override flags) to adjust them
without writing a config, or pass `-c <path>` to point at one. Naming a config
with `-c` that does not exist is an error.

## Output layout

```
<base_directory>/
  _users/<bucket>.json            users bucketed by id >> 13
  _users/pending.json
  <wiki>/
    http_cookies.json
    meta/site.json
    meta/sitemap.json
    meta/pages/<name>.json        page metadata (":" -> "_")
    meta/file_map.json  meta/page_id_map.json  meta/pending_*.json
    meta/forum/category/<id>.json
    meta/forum/<cat>/<thread>.json
    pages/<name>.7z               compressed page revisions (<rev>.txt)
    files/<page>/<file_id>        raw attachments
    forum/<cat>/<thread>.7z       compressed post html (<post>/<rev|latest>.html)
```

## Building

```
go build -o wikit ./cmd/wikit
```

Cross-compile (the matching 7-Zip binary is embedded per platform):

```
GOOS=windows GOARCH=amd64 go build -o wikit.exe ./cmd/wikit
GOOS=linux   GOARCH=amd64 go build -o wikit     ./cmd/wikit
```


## Tests

```
go test ./...
```
