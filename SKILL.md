---
name: berkeley-classes
description: "Search every Berkeley section, sync the full term offline, and build conflict-free schedules from the command line. Trigger phrases: `find a berkeley class`, `berkeley class schedule`, `is cs 61a open`, `build my berkeley schedule`, `what is teaching at berkeley`, `use berkeley-classes`, `run berkeley-classes`."
author: "ish-cs"
license: "MIT"
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
---

# UC Berkeley Class Schedule — CLI

## Prerequisites: Install the CLI

This skill drives the `berkeley-classes` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via Go:
   ```bash
   go install github.com/ish-cs/berkeley-classes-cli/cmd/berkeley-classes@latest
   ```
2. Verify: `berkeley-classes --version`
3. Ensure `$(go env GOPATH)/bin` (usually `$HOME/go/bin`) is on `$PATH`.

Requires Go 1.26 or newer. If `--version` reports "command not found" after install, the runtime cannot see the binary directory on `$PATH`. Do not proceed with skill commands until verification succeeds.

berkeley-classes mirrors the public schedule into local SQLite so you can search, filter, watch waitlists, and answer questions classes.berkeley.edu cannot — like 'build me a valid Fall schedule' or 'show every section John DeNero is teaching.'

## When to Use This CLI

Use when you need fast, structured answers about Berkeley sections, want to plan a non-conflicting schedule, watch enrollment changes, or feed class data to an LLM/agent. Local sync makes repeated queries instant.

## Anti-triggers

Do not use this CLI for:
- Do not use this CLI to enroll, drop, or modify your real schedule — it is read-only.
- Do not use it as a substitute for grade distributions or RateMyProf data — neither classes.berkeley.edu nor this CLI carry that.
- Do not use it to query archived terms beyond what classes.berkeley.edu currently exposes — there is no historical archive.

## Unique Capabilities

These capabilities aren't available in any other tool for this API.

### Local state that compounds
- **`schedule build`** — Build a valid weekly schedule from a wishlist of courses with no time overlaps.

  _When an agent helps a student plan a semester, this is the one-shot answer that beats clicking through 5784 sections by hand._

  ```bash
  berkeley-classes schedule build --term 'Fall 2026' --course 'COMPSCI 61A' --course 'MATH 1B' --course 'ENGLISH 45A'
  ```
- **`watch`** — Watch a CCN and report when open seats appear, waitlist shrinks, or capacity changes.

  _Enrollment shifts are minute-level events; agents and students cannot refresh the page every five minutes._

  ```bash
  berkeley-classes watch 29147 --interval 5m --max-checks 1
  ```
- **`since`** — Surface new sections, cancellations, instructor swaps, and enrollment moves since the last sync.

  _Tells an agent exactly what's new in a department since yesterday — no diffing required by the caller._

  ```bash
  berkeley-classes since --term 'Fall 2026' --hours 24
  ```
- **`dept`** — Aggregate a department's offerings this term: total sections, distinct courses, open seats, capacity, top instructors.

  _Quick scan of what a department is teaching this term — useful for agents helping students explore majors._

  ```bash
  berkeley-classes dept COMPSCI --term 'Fall 2026'
  ```

### Agent-native plumbing
- **`instructor`** — List every section a given instructor is teaching this term, across every subject.

  _Answers 'what is X teaching' without the user knowing which department X is in._

  ```bash
  berkeley-classes instructor 'John DeNero' --term 'Fall 2026'
  ```
- **`open`** — Show every open section of a course in one command, including waitlist length.

  _Highest-frequency enrollment question collapsed into one command an agent can call._

  ```bash
  berkeley-classes open 'COMPSCI 61A' --term 'Fall 2026'
  ```
- **`conflict`** — Check whether two CCNs conflict on day-of-week and time.

  _One-line answer to a question that otherwise needs visual comparison._

  ```bash
  berkeley-classes conflict 29147 32104
  ```
- **`compare`** — Render two sections side-by-side: title, instructors, meeting days/time, location, units, enrollment, plus a conflict verdict.

  _One command answers 'which section should I pick?' without two browser tabs._

  ```bash
  berkeley-classes compare 29147 29179
  ```

## Command Reference

**facets** — Enumerate terms, subjects, and other search facets

- `berkeley-classes facets` — Fetch the homepage facet sidebar listing all current terms and subject areas

**sections** — View section search and detail pages

- `berkeley-classes sections get` — Get a section by its detail-page slug, e.g. '2026-fall-compsci-61a-001-lec-001'
- `berkeley-classes sections list` — Fetch the raw HTML search results for keyword + facet filters


### Finding the right command

When you know what you want to do but not which command does it, ask the CLI directly:

```bash
berkeley-classes which "<capability in your own words>"
```

`which` resolves a natural-language capability query to the best matching command from this CLI's curated feature index. Exit code `0` means at least one match; exit code `2` means no confident match — fall back to `--help` or use a narrower query.

## Recipes

### Find every open CS section in Fall 2026

```bash
berkeley-classes find --subject 'Computer Science' --term 'Fall 2026' --open-only --agent --select sections.code,sections.title,sections.open_seats,sections.instructor
```

Filter to open seats and emit only the fields an agent needs.

### Watch CS 161 (CCN 29202) every 10 minutes

```bash
berkeley-classes watch 29202 --interval 10m
```

Polls until you Ctrl-C; reports any change in open seats or waitlist.

### Build a 16-unit Fall schedule

```bash
berkeley-classes schedule build --term 'Fall 2026' --course 'COMPSCI 61A' --course 'MATH 53' --course 'ENGLISH R1A' --course 'PHYSICS 7A' --max-results 5
```

Picks one section per course with no time overlap; prints up to 5 valid combinations.

### What is DeNero teaching this term?

```bash
berkeley-classes instructor 'John DeNero' --term 'Fall 2026' --agent
```

Cross-department instructor lookup, agent-friendly JSON.

## Auth Setup

No authentication required.

Run `berkeley-classes doctor` to verify setup.

## Agent Mode

Add `--agent` to any command. Expands to: `--json --compact --no-input --no-color --yes`.

- **Pipeable** — JSON on stdout, errors on stderr
- **Filterable** — `--select` keeps a subset of fields. Dotted paths descend into nested structures; arrays traverse element-wise. Critical for keeping context small on verbose APIs:

  ```bash
  berkeley-classes facets --agent --select id,name,status
  ```
- **Previewable** — `--dry-run` shows the request without sending
- **Offline-friendly** — sync/search commands can use the local SQLite store when available
- **Non-interactive** — never prompts, every input is a flag
- **Read-only** — do not use this CLI for create, update, delete, publish, comment, upvote, invite, order, send, or other mutating requests

### Response envelope

Commands that read from the local store or the API wrap output in a provenance envelope:

```json
{
  "meta": {"source": "live" | "local", "synced_at": "...", "reason": "..."},
  "results": <data>
}
```

Parse `.results` for data and `.meta.source` to know whether it's live or local. A human-readable `N results (live)` summary is printed to stderr only when stdout is a terminal AND no machine-format flag (`--json`, `--csv`, `--compact`, `--quiet`, `--plain`, `--select`) is set — piped/agent consumers and explicit-format runs get pure JSON on stdout.

## Agent Feedback

When you (or the agent) notice something off about this CLI, record it:

```
berkeley-classes feedback "the --since flag is inclusive but docs say exclusive"
berkeley-classes feedback --stdin < notes.txt
berkeley-classes feedback list --json --limit 10
```

Entries are stored locally at `~/.local/share/berkeley-classes/feedback.jsonl`. They are never POSTed unless `BERKELEY_CLASSES_FEEDBACK_ENDPOINT` is set AND either `--send` is passed or `BERKELEY_CLASSES_FEEDBACK_AUTO_SEND=true`. Default behavior is local-only.

Write what *surprised* you, not a bug report. Short, specific, one line: that is the part that compounds.

## Output Delivery

Every command accepts `--deliver <sink>`. The output goes to the named sink in addition to (or instead of) stdout, so agents can route command results without hand-piping. Three sinks are supported:

| Sink | Effect |
|------|--------|
| `stdout` | Default; write to stdout only |
| `file:<path>` | Atomically write output to `<path>` (tmp + rename) |
| `webhook:<url>` | POST the output body to the URL (`application/json` or `application/x-ndjson` when `--compact`) |

Unknown schemes are refused with a structured error naming the supported set. Webhook failures return non-zero and log the URL + HTTP status on stderr.

## Named Profiles

A profile is a saved set of flag values, reused across invocations. Use it when a scheduled agent calls the same command every run with the same configuration - HeyGen's "Beacon" pattern.

```
berkeley-classes profile save briefing --json
berkeley-classes --profile briefing facets
berkeley-classes profile list --json
berkeley-classes profile show briefing
berkeley-classes profile delete briefing --yes
```

Explicit flags always win over profile values; profile values win over defaults. `agent-context` lists all available profiles under `available_profiles` so introspecting agents discover them at runtime.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Usage error (wrong arguments) |
| 3 | Resource not found |
| 5 | API error (upstream issue) |
| 7 | Rate limited (wait and retry) |
| 10 | Config error |

## Argument Parsing

Parse `$ARGUMENTS`:

1. **Empty, `help`, or `--help`** → show `berkeley-classes --help` output
2. **Starts with `install`** → ends with `mcp` → MCP installation; otherwise → see Prerequisites above
3. **Anything else** → Direct Use (execute as CLI command with `--agent`)

## MCP Server Installation

1. Install the MCP server:
   ```bash
   go install github.com/ish-cs/berkeley-classes-cli/cmd/berkeley-classes-mcp@latest
   ```
2. Register with your MCP host (refer to your host's docs for the exact command).
3. The server speaks stdio by default; pass `--transport http --addr :7777` for remote / HTTP mode.

## Direct Use

1. Check if installed: `which berkeley-classes`
   If not found, offer to install (see Prerequisites at the top of this skill).
2. Match the user query to the best command from the Unique Capabilities and Command Reference above.
3. Execute with the `--agent` flag:
   ```bash
   berkeley-classes <command> [subcommand] [args] --agent
   ```
4. If ambiguous, drill into subcommand help: `berkeley-classes <command> --help`.
