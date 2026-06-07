# bcourses

**Search every Berkeley class from your terminal. Build conflict-free schedules. Watch waitlists.**

`bcourses` is a command-line tool for [classes.berkeley.edu](https://classes.berkeley.edu). It mirrors the full Schedule of Classes into a local SQLite database so you can search, filter, and reason about Berkeley sections faster than the website. It can also do things the website cannot — like building a conflict-free schedule from a wishlist of courses, or watching a class until a seat opens.

Built by a Berkeley student for Berkeley students.

---

## What it does

- **Search every section** by keyword, instructor, days, time window, units, course level, breadth requirement, open-seats-only, and more — all offline once you sync.
- **Build a conflict-free schedule** from a list of courses you want.
- **Watch a CCN** and get pinged when seats open or waitlists shrink.
- **Compare two sections** side-by-side (instructors, times, locations, enrollment, conflict verdict).
- **Department overview** — total sections, distinct courses, top instructors, open seats by department.
- **Find an instructor** across every department in one query.
- **What changed?** — track new sections, instructor swaps, and enrollment moves since your last sync.
- **JSON output** on every command (`--agent` flag) so you can pipe it into anything.

---

## Install

Requires [Go 1.26+](https://go.dev/dl/).

```bash
go install github.com/ish-cs/bcourses-cli/cmd/bcourses@latest
```

Verify:

```bash
bcourses --version
```

If `bcourses` isn't found, make sure `$(go env GOPATH)/bin` (usually `~/go/bin`) is on your `$PATH`.

---

## Quickstart

```bash
# 1. Check the live site is reachable
bcourses doctor

# 2. List the current terms
bcourses terms | grep "Fall\|Spring"

# 3. Pull a department offline (one-time, ~30s per dept)
bcourses sync run --term 'Fall 2026' --subject 'Computer Science'

# 4. Search what you just synced
bcourses find --keywords 'CS 61A' --term 'Fall 2026'

# 5. Build a conflict-free schedule
bcourses schedule build --term 'Fall 2026' \
  --course 'COMPSCI 61A' \
  --course 'MATH 1B' \
  --course 'ENGLISH R1A'
```

---

## Commands

### Discovery

| Command | What it does |
|---|---|
| `bcourses doctor` | Check live-site reachability + local cache health |
| `bcourses terms` | List every term (Fall 2026, Spring 2026, …) |
| `bcourses subjects` | List every subject area (COMPSCI, MATH, …) with section counts |

### Local sync (foundation for everything else)

```bash
bcourses sync run --term 'Fall 2026' --subject 'Computer Science'
```

Pulls all sections of one subject in one term into your local SQLite store. Run this once per subject you care about. Re-run anytime to refresh enrollment numbers.

### Search

```bash
bcourses find --keywords '61A' --term 'Fall 2026'
bcourses find --subject 'Computer Science' --open-only
bcourses find --instructor 'DeNero'
bcourses find --days MWF --after 10am --before 4pm
bcourses find --level upper --units 4
```

### Transcendence — things the website can't do

| Command | Example | What it does |
|---|---|---|
| `schedule build` | `bcourses schedule build --term 'Fall 2026' --course 'COMPSCI 61A' --course 'MATH 1B'` | Builds the first N valid weekly schedules with no time conflicts |
| `watch <CCN>` | `bcourses watch 29147 --interval 5m` | Polls until open seats appear or waitlist changes |
| `since` | `bcourses since --term 'Fall 2026' --hours 24` | Shows what changed since your last sync (new sections, cancellations, instructor swaps) |
| `instructor <name>` | `bcourses instructor 'DeNero' --term 'Fall 2026'` | Every section an instructor is teaching, across every department |
| `open <code>` | `bcourses open 'COMPSCI 61A'` | Every open section of one course |
| `conflict <A> <B>` | `bcourses conflict 29147 29122` | Do these two CCNs collide on day-of-week and time? |
| `compare <A> <B>` | `bcourses compare 29147 29179` | Side-by-side render of two sections with conflict verdict |
| `dept <code>` | `bcourses dept COMPSCI --term 'Fall 2026'` | Department overview: section count, open seats, top instructors |

---

## Recipes

**Find every open CS section in Fall 2026:**

```bash
bcourses find --subject 'Computer Science' --term 'Fall 2026' --open-only
```

**Build a 16-unit Fall schedule:**

```bash
bcourses schedule build --term 'Fall 2026' \
  --course 'COMPSCI 61A' \
  --course 'MATH 53' \
  --course 'ENGLISH R1A' \
  --course 'PHYSICS 7A' \
  --max-results 5
```

**Watch CS 161 every 10 minutes:**

```bash
bcourses watch 29202 --interval 10m
```

**What is DeNero teaching this term?**

```bash
bcourses instructor 'John DeNero' --term 'Fall 2026'
```

**Department snapshot:**

```bash
bcourses dept COMPSCI --term 'Fall 2026'
```

---

## Output modes

- **Default:** human-readable table.
- **`--json`** — pretty JSON.
- **`--agent`** — JSON + compact + no prompts + no color. The mode to use when piping into other tools or LLMs.
- **`--csv`** — CSV (for table/array responses).
- **`--select id,name,status`** — pick only the fields you want.

---

## How it works

1. **Sync** scrapes the public `classes.berkeley.edu` search pages, parses the section cards, and writes them into a local SQLite database (`~/.local/share/bcourses/data.db`).
2. **Queries** run against that local DB. Most commands are instant.
3. **Watch/since** keep historical snapshots so they can diff against the last poll.

There is no Berkeley API key required. No login. No private data. Everything `bcourses` reads is public on `classes.berkeley.edu`.

---

## Limitations & anti-triggers

- **Read-only.** `bcourses` cannot enroll, drop, or modify your schedule. Use CalCentral for that.
- **No grade distributions or RateMyProf data** — that's not on `classes.berkeley.edu`, so it's not here either. Use [berkeleytime.com](https://berkeleytime.com) for grades.
- **No historical archive** beyond what `classes.berkeley.edu` currently exposes.
- **Term-scoped:** the local store is per-term. Sync the term you want to search.

---

## Contributing

Found a bug? Missing a feature? Open an issue or PR. This is for Berkeley students by a Berkeley student.

```bash
git clone https://github.com/ish-cs/bcourses-cli.git
cd bcourses-cli
go test ./...
go build -o bcourses ./cmd/bcourses
```

---

## License

MIT. See [LICENSE](LICENSE).
