---
name: backlog
description: CLI-referens för Backlog-verktyget - kommandon och flaggor för tasks, planer, kommentarer, etiketter, projekt, memory, docs och bilagor i en lokal SQLite-workspace. Ren referens utan arbetsflöde: flödena ägs av todo-capture (fånga todos), backlog-loop (beta av en task med Judge-grind) och review-to-backlog (granskningsfynd till tasks).
---

You have access to the `backlog` CLI. Use it to manage tasks, plans, comments, labels, projects, memory entries, docs, and attachments. Always pass `--as ai:<your-model-name>` so writes are attributed to you. Always pass `--json` when you need to parse output.

## Session startup

Sessionsstart-flödet (läsa in memory, docs och öppna tasks) ägs av
backlog-memory-skillen - se den. Kommandona är `backlog memory list`,
`backlog doc list`/`doc show` och `backlog task list`, alla med
`--project <alias> --json --profile default`.

## Arbetsflöden (lokalt)

Ingen agentisk loop här - arbetsflödena ägs av andra skills:
`todo-capture` (fånga och gå vidare, jobba aldrig todon direkt),
`/backlog-loop` (beta av EN task med Judge-grind),
`review-to-backlog` (granskningsfynd till tasks).
Den här skillen är enbart kommandoreferens.

## Memory workflow

Använd den lokala backlog-memory-skillen (learn/store). Delningsregeln:
projektfakta och beslut till backlog memory, agentbeteende-feedback till
agentens egen memory.

## Conventions

- Always `--profile default` unless the user specifies otherwise.
- Always `--as ai:<your-model-name>` on writes.
- Always `--json` when parsing output.
- Use `TASK-N` format in messages to the user.
- Never hardcode actor names - use `ai:<your-model-name>` dynamically.

## Core concepts

| Concept | Description |
|---|---|
| Workspace | Directory containing `backlog.db` + `config.toml`. Resolved via `--db`, `$BACKLOG_DB`, `--profile`, or the default profile. There is **no** cwd walk-up. |
| Profile | Named pointer to a workspace, registered in `~/.config/backlog/config.toml`. Lokalt på den här maskinen pekar profilen `default` på `~/.backlog/default/` (kontrollera med `backlog profile show default`). |
| Project | Named group of tasks inside a workspace, identified by a short `alias` (e.g. `api`, `web`). |
| Task | Unit of work. Has type, status, priority, actor. Identified by `TASK-N`, bare `N`, or full ULID. |
| Plan | Versioned markdown document attached to a task. Every edit creates a new immutable version. |
| Doc | Versioned markdown document attached to a **project** (not a task). Same versioning model as plans. |
| Memory | Free-form note attached to a project. Body + comma-separated tags. Newest-first list, tag filter. |
| Attachment | Binary file attached to a task or a doc, stored in the SQLite DB. |
| Comment | Actor-attributed note on a task. |
| Label | Per-project tag attachable to tasks. |
| Actor | `kind:name` - `kind` is `human` or `ai`. Example: `ai:claude-code`. |

## ID formats - all equivalent for tasks

```
TASK-5          # canonical human-readable ref
5               # bare integer
01KR4JA4754H... # full ULID (returned in JSON)
```

Use the `TASK-N` format in messages to users. Use the ULID from JSON output when chaining commands. Plan IDs, doc IDs, attachment IDs, and memory IDs are always full or short ULIDs - there is no `PLAN-N` form.

## Global flags (apply to every command)

```
--json          output machine-readable JSON (always use when parsing)
--as kind:name  actor attribution, e.g. --as ai:<modell>
--db <path>     explicit path to backlog.db
--profile <n>   named workspace profile
--quiet         suppress success messages
```

## Projects

```sh
# Create
backlog project add "API Service" --alias api
backlog project add "Web Frontend" --alias web --repo-path /code/web

# List
backlog project list --json
backlog project list --include-archived --json

# Show
backlog project show api --json

# Update
backlog project update api --name "API v2" --description "REST backend"

# Archive (hidden from list; tasks remain)
backlog project archive api

# Delete (hard - removes all tasks, plans, comments, labels)
backlog project delete api
```

JSON shape of a project:
```json
{ "id": "...", "alias": "api", "name": "API Service", "description": "", "repo_path": "", "created_at": 0, "updated_at": 0 }
```

---

## Tasks

### Create

```sh
backlog task add \
  --project api \
  --title "Fix SQL injection in /search" \
  --description "Parameterize the query at internal/handlers/search.go:84" \
  --type vulnerability \
  --priority P1 \
  --source "semgrep" \
  --external-ref "SEMGREP-042" \
  --label security \
  --as ai:<modell>
```

Flags:
- `-p / --project` alias (required)
- `-t / --title` (required)
- `-d / --description` markdown body
- `--type` task · bug · issue · improvement · feature · vulnerability · chore · spike · bucket-list
- `--priority` P1–P5 or 1–5 (P1 = highest, P3 = default)
- `--status` todo · doing · done (default: todo)
- `--assignee` name
- `--label` repeatable: `--label auth --label crypto`
- `--source` origin tool/review name
- `--external-ref` URL or ticket ID
- `--project-path` fil:rad eller URL till relevant kodställe (t.ex. `internal/handlers/search.go:84`)
- `--from-file <file>` - `.json` is parsed as a full task payload, anything else is loaded as the description
- `--due-date` YYYY-MM-DD or RFC3339

### List

```sh
backlog task list --json
backlog task list --project api --json
backlog task list --status todo --json
backlog task list --type vulnerability --priority P1 --json
backlog task list --label security --json
backlog task list --actor-kind ai --json
backlog task list --actor-name <modell> --json
backlog task list --source semgrep --json
backlog task list --search "injection" --json    # FTS5
backlog task list --search "sql*" --json         # prefix
backlog task list --include-archived --json
backlog task list --limit 20 --offset 0 --json
backlog task list --sort seq --json           # sort by TASK-N order
backlog task list --sort created --json       # newest first
backlog task list --sort updated --json
backlog task list --sort title --json
```

JSON response shape (CLI):
```json
{
  "tasks": [
    {
      "id": "01KR...",
      "seq": 1,
      "project_id": "...",
      "project": { "alias": "api", "name": "API Service" },
      "title": "Fix SQL injection",
      "type": "vulnerability",
      "status": "todo",
      "priority": 1,
      "actor": { "kind": "ai", "name": "<modell>" },
      "source": "semgrep",
      "external_ref": "SEMGREP-042",
      "labels": [{ "name": "security", "color": "" }],
      "created_at": 1746724800000000000,
      "updated_at": 1746724800000000000
    }
  ],
  "page": { "total": 12, "count": 12 }
}
```

### Show

```sh
backlog task show TASK-1 --json
backlog task show TASK-1 --with-plans=false --with-comments=false --json   # disable defaults
```

`--with-plans` and `--with-comments` are both `true` by default.

### Update

Only provided flags are changed:
```sh
backlog task update TASK-1 --title "New title" --priority P2 --as ai:<modell>
backlog task update TASK-1 --assignee alice --source pentest-report
backlog task update TASK-1 --due-date 2026-06-01
```

### Move status

```sh
backlog task move TASK-1 --status doing --as ai:<modell>
backlog task move TASK-1 --status done  --as ai:<modell>
backlog task move TASK-1 --status todo  --as ai:<modell>
backlog task transfer TASK-1 TASK-2 --to other-alias   # move to another project; TASK-N and labels follow
```

### Archive / Delete

```sh
backlog task archive TASK-1   # soft - hidden from list, recoverable via --include-archived
backlog task delete  TASK-1   # hard - permanent
```

---

## Plans

Plans are versioned. Every `plan update` creates a new immutable version. The current version is always returned by default.

### Add plan (creates v1)

```sh
backlog plan add \
  --task TASK-1 \
  --title "Remediation plan" \
  --content "## Steps\n1. Replace string interpolation with parameterized queries\n2. Add regression test" \
  --as ai:<modell> --json
```

To load body from a file:
```sh
backlog plan add --task TASK-1 --title "Plan" --from-file plan.md --as ai:<modell>
```

JSON response:
```json
{
  "id": "01KR...",
  "task_id": "...",
  "current_version": 1,
  "version": {
    "id": "...", "version": 1,
    "title": "Remediation plan", "body": "## Steps\n...",
    "actor": { "kind": "ai", "name": "<modell>" },
    "created_at": 0
  }
}
```

### Update plan (creates v2, v3, …)

```sh
backlog plan update <plan-id> \
  --title "Revised plan" \
  --content "## Steps\n1. Parameterize queries\n2. Rotate credentials\n3. Add regression test" \
  --change-note "added credential rotation" \
  --as human:alice --json
```

`--title` is required on update; `--from-file` works the same as on `add`.

### Show, history, list, delete

```sh
backlog plan show <plan-id> --json              # current version
backlog plan show <plan-id> --version 1 --json
backlog plan history <plan-id> --json
# { "versions": [ { "version": 1, "title": "...", "actor": {...}, "change_note": "", "created_at": 0 }, ... ] }

backlog plan list --task TASK-1 --json
backlog plan delete <plan-id>
```

---

## Comments

```sh
backlog comment add "Verified fix in PR #142." --task TASK-1 --as ai:<modell>
backlog comment list --task TASK-1 --json
backlog comment delete <comment-id>
```

The body is a positional argument; `--task` is a required flag.

---

## Labels

```sh
# Create (per-project)
backlog label create "security" --project api --color "#ff0000"

# List
backlog label list --project api

# Attach / detach (use ULID, not TASK-N - task ref is resolved internally)
backlog label attach security --task TASK-1
backlog label detach security --task TASK-1
```

---

## Memory (project-scoped notes)

Free-form text + optional comma-separated tags. Use this for decisions, context, design notes that aren't a task.

```sh
# Add
backlog memory add "Decided to use SQLite - single file, no server" \
  --project api --tag "decision,arch" --as ai:<modell>

# Append text to an existing entry (newline-joined)
backlog memory append <memory-id> "Confirmed at 2026-05-09 review meeting"

# List
backlog memory list --project api --json
backlog memory list --project api --tag arch --json   # filter by tag
backlog memory export --project api -o memory.md       # full text as Markdown; --all for every project

# Delete
backlog memory delete <memory-id>
```

JSON entry shape:
```json
{
  "id": "01KR...",
  "project_id": "...",
  "body": "Decided to use SQLite - single file, no server",
  "tags": "decision,arch",
  "actor": { "kind": "ai", "name": "<modell>" },
  "created_at": 1746724800000000000
}
```

---

## Docs (project-scoped versioned documents)

Like plans, but attached to a project and used for longer-form documentation (architecture overviews, runbooks, design docs).

```sh
# Add (creates v1)
backlog doc add --project api --title "Architecture Overview" \
  --content "## Stack\n- Go 1.25\n- SQLite" --as ai:<modell> --json

backlog doc add --project api --title "Runbook" --from-file runbook.md

# List
backlog doc list --project api --json

# Show current version
backlog doc show <doc-id> --json

# Update (creates a new version)
backlog doc update <doc-id> --title "Architecture v2" \
  --content "..." --change-note "added queue layer" --as ai:<modell>

# Append (creates a new version with old body + new body joined)
backlog doc append <doc-id> --content "## New section\n..." \
  --change-note "extended troubleshooting"

# History
backlog doc history <doc-id> --json

# Delete
backlog doc transfer <doc-id> --to other-alias   # move doc with history to another project
backlog doc delete <doc-id>
```

`--from-file` is supported on both `add`, `update`, and `append`.

### The workspace is shared - `update` replaces the whole body

The backlog DB is shared across the whole machine, and several sessions (and
several models) can work in the same project at once. Docs are a shared
resource, not a file in your worktree.

- **`update` replaces the ENTIRE body**, whether the content comes from
  `--content` or `--from-file`. There is no merge, no conflict, no question -
  just a new version containing exactly what you sent. Anything another
  session added since you last read the doc is gone.
- **`append` only adds**, so it cannot clobber anyone. Use it for additions.
- **`history` shows who wrote what and when.** Read it whenever a doc looks
  different than you remember - the author column tells you if another
  session has been in it.

Practical rule: additions go through `append`. To change an existing passage,
`show` the doc immediately before editing and write it back straight away -
the window between reading and writing is the entire risk. Never update from a
copy that has been sitting in a scratch directory while you did other work.

Real case, 2026-08-18: a doc was taken from v10 to v13 by another model while
a local copy from v10 sat unused. One more `update --from-file` from that copy
would have silently erased three versions of someone else's work.

---

## Activity log

```sh
backlog activity               # show last 50 events
backlog activity --limit 20    # limit results
backlog activity --json        # machine-readable
```

---

## Common agent workflows

### Triage findings from a scan

```sh
# 1. Write findings.json from scan output
# 2. Dry run
backlog import-findings findings.json --dry-run
# 3. Import
backlog import-findings findings.json --as ai:scanner
# 4. Confirm
backlog task list --actor-name scanner --json
```

### Pick up a task and attach a plan

```sh
backlog task move TASK-5 --status doing --as ai:<modell>
backlog plan add --task TASK-5 --title "Implementation plan" \
  --content "..." --as ai:<modell> --json
# → capture plan ID from response
```

### Revise a plan after human feedback

```sh
backlog plan update <plan-id> \
  --title "Revised plan" \
  --content "..." \
  --change-note "incorporated Alice's review" \
  --as ai:<modell> --json
```

### Capture a design decision as memory

```sh
backlog memory add "Chose Cobra over urfave/cli - better completion + posix flag handling" \
  --project api --tag "decision,deps" --as ai:<modell>
```

### Maintain a versioned runbook

```sh
backlog doc add --project api --title "Incident Runbook" \
  --from-file runbook.md --as ai:<modell>
backlog doc append <doc-id> --content "## 2026-05 outage RCA\n..." \
  --change-note "post-mortem section" --as ai:<modell>
backlog doc history <doc-id> --json
```

### Scripting with JSON output

```sh
# Get all open P1 vulnerabilities and iterate
backlog task list --type vulnerability --priority P1 --status todo --json \
  | jq -r '.tasks[].id'

# Create a task and capture its ID
TASK_ID=$(backlog task add -p api -t "Fix XSS" --type vulnerability --priority P2 \
  --as ai:<modell> --json | jq -r '.id')

# Add a plan using the captured ID
backlog plan add --task "$TASK_ID" --title "Plan" --content "..." --as ai:<modell>
```

---

## Enum reference

**type:** `task` · `bug` · `issue` · `improvement` · `feature` · `vulnerability` · `chore` · `spike` · `bucket-list`

**status:** `todo` · `doing` · `done`

**priority:** `1`/`P1` (critical) · `2`/`P2` (high) · `3`/`P3` (normal, default) · `4`/`P4` (low) · `5`/`P5` (backlog)

**actor.kind:** `human` · `ai`

**sort (task list):** `priority` (default) · `created` · `updated` · `seq` · `title`

## Sällan använda kommandon

Init, profiler, bilagor, import-findings (format och bulkintag), import mellan
arbetsytor, export, sync, webb-UI, schema, shell completion, doctor, MCP-server,
miljövariabler och DB-upplösning står inte här. Kör `backlog <kommando> --help`,
eller läs backlog-docen "backlog CLI: sällan använda kommandon" i projektet
infra (`backlog doc list --project infra --profile default`).
