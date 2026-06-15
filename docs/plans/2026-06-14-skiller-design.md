# Standalone Skill-Installer — Design

Status: **DESIGN / CONVERGED — name locked: `skiller`**
Date: 2026-06-14
Authors: claude (`claude:529d0454`) + codex (`codex:b71174a5`), converged over the Talking Stick.
Name: **`skiller`** — binary `skiller` (suggested short alias `skl`), module `github.com/mostlydev/skiller`. See §13.
Origin: extract and generalize talking-stick's `src/install*.ts` + `src/skill-install.ts` + `src/harness-model.ts`.

---

## 1. Problem

We maintain four tools that each need to install an Agent Skill (a `SKILL.md` folder)
into one or more agent "harnesses" reliably:

| Tool | Language | Skill-install need |
| --- | --- | --- |
| `talking-stick` | Node/TS | skill → shared `~/.agents/skills` + Claude proprietary + a Grok hook |
| `our-ai` | Go | skill install |
| `clawdapus` | Go | skill install into **materialized/containerized** runtimes (OpenClaw, Hermes) |
| `gnit` | Rust | skill install |

Today the logic lives only inside talking-stick (TypeScript). It encodes hard-won
behavior — shared-vs-proprietary target resolution, symlink-vs-copy, duplicate
cleanup, idempotence, atomicity, migrations. The other three tools need the same
guarantees but cannot reuse a TypeScript module.

**Goal:** one reusable installer with one source of truth for harness knowledge and
install semantics, usable from Go, Rust, and Node without dragging a foreign runtime
onto the host.

## 2. Prior art (researched)

- **`vercel-labs/skills`** (`npx skills`, skills.sh) — 22.4k★, actively maintained.
  Installs `SKILL.md` folders into 70+ agents, symlink/copy, project+global scopes,
  auto-detection, self-update, lockfile. Implements the open **Agent Skills spec**
  (agentskills.io, originally from Anthropic). **It is the tool the operator
  half-remembered, and it is excellent — but it is TypeScript / `npm package skills`,
  `bin/cli.mjs`, `engines.node >=18`.** Adopting it as the shared dependency would
  force a Node.js runtime onto our Go and Rust tools — the exact cross-language
  dependency we must avoid. Its agent registry is *code* (`src/agents.ts`) with a
  murky license (README says MIT; no `LICENSE` file; `gh licenseInfo` null), so we do
  **not** vendor it.
- **Agent Skills spec** (agentskills.io / `github.com/agentskills/agentskills`) — the
  open `SKILL.md` format (folder + `name`/`description` frontmatter + optional
  scripts/references/assets). **We adopt this format wholesale.** Vercel's tool and
  70+ agents already speak it, so our output stays compatible.
- **Binary distribution/self-update** — `GoReleaser`, `go-selfupdate`, `eget`/`ubi`/
  `aqua`/`mise`. These solve binary bootstrap/update/distribution, **not** skill-install
  semantics. We lean on them for the boring distribution layer and own the semantics.

**Conclusion:** don't reinvent the *format* (adopt Agent Skills), don't adopt the
*Node tool* as a runtime dependency, don't hand-maintain 70 agents. Build a small
static binary that owns the install *semantics* for our verified fleet, with clean
extension points to grow later (and to optionally contribute a machine-readable
registry / Antigravity / a static binary upstream to Vercel).

## 3. Decision (operator-locked)

**A standalone static Go binary** is the integration contract. No interpreter
dependency on any host; all four tools `exec` it. The same logic is also exposed as a
**Go library core** so the two Go tools may import it directly and skip the subprocess.

Rejected: "adopt `npx skills` everywhere" (Node dependency on Go/Rust tools) and the
hybrid (two code paths + two drifting agent tables).

## 4. Goals / non-goals (v1)

**Goals**
- One binary + one Go library, one embedded harness registry, one manifest format.
- A persistent local state ledger that records observed installs, provenance, conflicts,
  and remembered conflict resolutions across skiller and other detected installers.
- Reliable, **idempotent**, **atomic** installs; safe uninstall that never deletes
  foreign content.
- Two target modes: **named harness targets** (autodetected) and **explicit
  `--target-dir` runtime profiles** (for materialized containers).
- Install a skill from **any source** — local dir, Git/GitHub URL, web `SKILL.md`, or
  archive — with recorded provenance (§6.9) so `update` can re-fetch and refresh it later.
  **No central source registry**: provenance is per-install, not a global marketplace.
- `--json` dry-run (`plan`) for every mutating command.
- Self-update + easy bootstrap.

**Non-goals (v1)**
- Not modeling all 70 agents — only our verified fleet (§6.1).
- Not modeling Hermes/OpenClaw as host-autodetected harnesses — they are runtime
  target-dir profiles (§6.2).
- No skill *authoring*/registry/discovery marketplace (that's Vercel's `skills find`).
- No project-committed scope work beyond what the manifest needs (global scope first;
  `project_dir` is in the registry data but lower priority).
- No central database as the only authority for destructive operations. The ledger is
  persistent memory and conflict UX; target-local markers and realpaths remain the
  safety proof for writes and deletes.

## 5. Architecture

```
skiller/                 (new repo)
├── go.mod
├── pkg/                     library core — the contract for Go consumers
│   ├── registry/            embedded harness registry (go:embed data + typed loader)
│   ├── manifest/            TOML manifest parse/validate
│   ├── source/              resolve a SourceSpec -> SourceSnapshot (fetchers: file/git/http)
│   ├── observe/             impure FS scan -> WorldState (ownership-class + digest per root)
│   ├── plan/                pure: (manifest, registry, WorldState, opts) -> Plan{actions[]}
│   ├── install/             apply Plan: link/copy, atomic, idempotent, marker write
│   ├── prune/               duplicate cleanup + safe uninstall (marker/realpath gated)
│   ├── state/               JSON ledger: observed installs, conflicts, remembered decisions
│   ├── status/              derived inspection/index views over state + target roots
│   └── selfupdate/          version check + update
├── cmd/skiller/         thin CLI over pkg/ (cobra/std flag)
├── internal/                fsutil (atomic rename, symlink+fallback), digest, lock
├── data/harnesses.toml      embedded harness registry (single source of truth)
├── data/markers.toml        known ownership markers: ours-legacy + foreign (§6.7)
├── data/sources.toml        host shorthands / source detectors (§6.9)
└── .goreleaser.yaml         multi-platform static builds + checksums
```

- **Observe / plan / apply is the spine.** `pkg/observe` performs the only reads that
  feed a decision: it scans each candidate root into a `WorldState` snapshot
  (ownership-class + digest per path). `pkg/plan` is then **pure** —
  `(manifest, registry, WorldState, options) -> Plan{actions[]}` with no I/O at all — so
  conflict detection and the whole decision table (§6.7) are golden-/table-testable. This
  is the separation that made talking-stick's `planSkillInstall` reviewable, taken one
  step further: even the filesystem reads are lifted out of the planner.
- **`pkg/source` resolves first.** A `SourceSpec` (local path, Git/GitHub, web file, or
  archive) is normalized and fetched into a `SourceSnapshot` (§6.9) before anything else.
  Observe and plan consume snapshots, never raw URLs, so source fetching is the one
  network layer and stays cleanly outside the pure planner.
- **`pkg/install` applies** a `Plan` and is the only writer. Idempotence and atomicity
  live here. Resolve (fetch), observe (read), and apply (write) are the impure layers;
  `pkg/plan` stays pure.
- CLI is a thin shell: parse args → build options → call `pkg`. Go tools can skip the
  CLI and call `pkg` directly.

## 6. Core concepts

### 6.1 Harness registry (embedded data, not hardcoded Go)

Ported from talking-stick's `src/harness-model.ts` model, but stored as an embedded
data file so it's trivially extensible and could become the machine-readable registry
the ecosystem currently lacks.

```toml
# data/harnesses.toml
[[harnesses]]
id = "agents"                 # canonical shared target
kind = "shared"
global_dir = "~/.agents/skills"
project_dir = ".agents/skills"
readers = ["codex", "antigravity", "grok", "opencode"]

[[harnesses]]
id = "claude-code"
kind = "proprietary"
global_dir = "~/.claude/skills"
env_home = "CLAUDE_CONFIG_DIR"

```

`kind ∈ {shared, proprietary, shared-reader}` in v1. `shared` is the canonical
`agents` group; `proprietary` is a harness with its own private root (e.g.
`claude-code`); `shared-reader` is a harness that primarily reads a shared group
(`primary_target = "agents"`) yet keeps its own proprietary duplicate roots for
detection/cleanup (codex, grok, antigravity, opencode). Loader is typed; unknown
fields are a validation error so the data and code can't silently drift (mirrors the
talking-stick rule that derived harness lists must not drift, and enforced by
`decodeStrictTOML`'s `Undecoded()` check — the typed loader *is* the data schema, so
no parallel JSON Schema for the TOMLs). The real registry needs a few more fields
than the sketch: aliases/readers, global and project dirs, env-home overrides, detect
dirs, duplicate-cleanup dirs, primary target, default mode, and whether the target can
carry extras. These remain data-only and are covered by fixture-`HOME` tests.

**Gemini is removed from the new installer.** Google says Gemini CLI stops serving
consumer requests on **June 18, 2026**. Any legacy Gemini cleanup/delegation belongs in
the adopting tool's transition code, not in this shared core.

### 6.2 Two target modes

1. **Named harness targets** — `--target agents,claude-code` (or manifest `targets`).
   Resolved through the registry to real dirs; `agents` is the canonical shared dir.
   This is the laptop/host path with autodetection. Harness names that read the same
   shared location are aliases to a **group**, not independent write targets: selecting
   `codex,antigravity,grok,opencode` collapses to one action against
   `~/.agents/skills/<skill>`, while status can still report each reader as satisfied
   by that shared target.
2. **Explicit runtime target-dir** — `--target-dir <path> --scope runtime` with **no
   autodetection and no registry lookup**. For materialized containers:

   ```
   skiller install \
     --source ./skills/foo --name foo \
     --target-dir .claw-skills/desk-manager/skills \
     --mode copy --owner clawdapus --scope runtime --json
   ```

   clawdapus then mounts that backing dir to the driver-reported `SkillDir`
   (`/claw/skills` for OpenClaw, `hermesHome/skills` for Hermes). **Hermes/OpenClaw are
   runtime profiles in v1, not host harnesses.** This mode is the spine that makes the
   tool reusable beyond laptops. Runtime target-dir mode defaults to **copy**, not
   link: a symlink inside `.claw-skills/...` that points at `~/.local/share/...` or a
   repo checkout can be broken inside the container unless that source path is mounted
   at the same location. `--mode link` is allowed only as an explicit opt-in with a
   warning.

   Before writing any named target, the planner surveys every root read by the in-scope
   harnesses — each selected harness's shared and proprietary roots — for the same
   effective skill name, using the registry plus the state ledger's remembered locations
   (not literally every root on disk; unrelated harnesses are not scanned). This applies
   even when a target is foreign or unmarked: if a direct CLI install already satisfies
   the desired skill for a reader, `skiller` must not create a duplicate in another root
   that same reader also loads, just because the existing directory lacks a skiller marker.

   Satisfaction is per reader, not just per path. For shared-reading harnesses, the
   planner also inspects proprietary duplicate roots that the selected readers may load
   (`~/.codex/skills`, `~/.grok/skills`, old OpenCode roots, etc.). A foreign matching
   skill in a proprietary root can satisfy that one reader, but it does not satisfy the
   whole shared `agents` group. Installing the shared copy anyway may create a duplicate
   for that reader, so the plan must surface a partial-satisfaction/conflict decision
   instead of blindly writing `~/.agents/skills/<skill>`.

   Boundary: runtime target-dir mode installs Agent Skill directories. It should not
   absorb every runtime file that a tool happens to call a "skill". In clawdapus, flat
   service-surface markdown files, generated `CLAWDAPUS.md`, `claw.describe`, and
   `claw.skill.emit` projection remain clawdapus responsibilities in v1. `skiller`
   handles bundled CLI/self skills and any runtime target whose layout is truly
   `skills/<name>/SKILL.md` or an equivalent driver-declared Agent Skill layout.

### 6.3 Skill manifest (TOML)

A consuming tool ships a manifest; the installer reads it. (Drafted by Codex.)

```toml
schema = "skiller-install.v1"
owner = "talking-stick"       # installer owner / package owner
namespace = "mostlydev"       # canonical provenance namespace
version = "0.4.14"
default_mode = "link"          # link | copy

[[skills]]
name = "talking-stick"
canonical_id = "mostlydev:talking-stick"   # optional stable owner:name identity
install_slug = "talking-stick"             # harness-visible directory/effective name
source = "./skills/talking-stick"          # a SourceSpec (§6.9): local path, git, or web URL
targets = ["agents", "claude-code"]   # agents == canonical shared ~/.agents/skills
mode = "link"
# A remote skill is just a different source spec — manifest shape is unchanged:
#   source = "github.com/acme/skills//debugging?ref=v1.2.0"   # repo // subdir ? ref

[[extras]]                     # generic file placement for tool-specific extras
id = "grok-session-hook"
harness = "grok"
source = "./integrations/grok/talking-stick-session.json"
target = "~/.grok/hooks/talking-stick-session.json"
mode = "copy"
owned_marker = true
```

If `canonical_id` is omitted, it is derived as `<namespace>:<name>`. Namespace default
order: CLI `--namespace`, env `SKILLER_NAMESPACE`, manifest `namespace`, local
config default, then `owner`. `install_slug` defaults to `name`. The `source` field is a
**SourceSpec** (§6.9): a local path today, or a `git`/GitHub/web/archive spec — the
manifest format does not change when a tool ships a remote-sourced skill.

`[[extras]]` keeps tool-specific *data* (talking-stick's Grok hook) in the generic
installer while tool-specific *logic* (instructions-file harness extraction, MCP
cleanup) stays in the owning tool. Directory extras can use the same in-directory
marker as skill copies. Single-file extras need a sidecar marker next to the target
(for example `talking-stick-session.json.skiller-install.json`) so uninstall and
sync remain safe without depending on the state ledger alone.

### 6.4 Install modes

- **link** (default for host named targets): symlink target → managed source. Single
  source of truth, updates for free. **Fallback to copy** when the filesystem/OS can't
  symlink (Windows without privilege, some mounted FS) — recorded in the plan so it's
  visible.
- **copy**: independent copy + ownership marker (§6.5).
- **runtime default**: `--scope runtime` / `--target-dir` defaults to copy for container
  visibility reasons (§6.2). Link is opt-in only.

### 6.5 Ownership & safety (Codex's scheme)

- **Symlink installs**: ownership proven by `realpath(target) == managed source`
  (talking-stick's `sameRealPath`). Sufficient; no marker needed.
- **Copy installs**: write `.skiller-install.json` **inside the installed skill
  dir** (not a sidecar, not `SKILL.md` frontmatter — frontmatter would mutate upstream
  content and corrupt digests). Contains: installer name/version, `owner`, skill name,
  source realpath (when known), `installed_at`, `mode`, `target_kind`, optional
  manifest path, `source_digest_at_install`, and `installed_digest_at_install`.
  Directory digests exclude the marker itself and other installer metadata.
- **Copy drift states**:
  - `STALE`: `current_digest == installed_digest_at_install` and desired digest differs;
    safe stale managed copy; replace on sync.
  - `MODIFIED`: `current_digest != installed_digest_at_install`; locally modified copy;
    preserve unless `--force`.
  - `current_digest == desired_digest` but marker schema is old: repair marker only.
- **Conservative delete**: remove a copy only if marker `owner` matches **and** the
  current digest still equals `installed_digest_at_install`, unless `--force` was
  passed. Never touch dirs we don't own.
- **Foreign satisfaction**: if a foreign or unmarked target already exists at the
  desired effective name, compute its digest before deciding it is a conflict. When the
  current target digest equals the desired source digest, the plan records
  `satisfied-by-foreign`, performs no write, and does not add
  a marker to the foreign directory.
- **Duplicate cleanup**: port talking-stick's symlink-only prune — remove only symlinks
  that resolve to the managed source; preserve foreign symlinks and unmanaged copies.
- **Idempotence**: re-running install with the same inputs is a no-op (quiet), matching
  talking-stick's "quiet no-op runs."
- **Atomicity**: stage in a temp dir on the same filesystem, then atomic `rename`;
  acquire a per-target lock to serialize concurrent runs. Replacing a non-empty
  existing directory uses a staged replacement path and a best-effort rollback path,
  never an in-place partial copy.
- **Lock before observe (TOCTOU)**: for a *mutating* command, the per-target write
  lock(s) are acquired **before** `pkg/observe` scans, not just at apply time, and
  held through apply. Otherwise a concurrent process could mutate a root between the
  observe snapshot and the write, and the plan would apply against a stale `WorldState`.
  When more than one target root is involved, lock ids are acquired in deterministic
  sorted canonical order. No state or source-cache lock may be held while waiting for
  target locks. Read-only commands (`plan`, `status`, `sources list`) take no target
  write lock. See §12.

### 6.6 Improvements over current implementations

This is not just a port of any one tool's current installer:

- **Three-way copy digests** improve on our-ai's current `.our-managed.json` model,
  which can tell that a copy differs from source but cannot distinguish user edits from
  a stale managed copy.
- **Atomic replacement** improves on current remove-then-copy flows that leave the
  target absent if a process dies between removal and rewrite.
- **Shared-target grouping** improves on per-harness loops that create duplicate
  `~/.codex`, `~/.grok`, or `~/.opencode` entries even when the harnesses all read
  `~/.agents/skills`.
- **Runtime target-dir mode** covers Claw-managed OpenClaw/Hermes materialization
  without pretending those runtimes are normal host harnesses.
- **Canonical identity** is distinct from install slug. A manifest can carry a stable
  `canonical_id` such as `org:skill-name` while the installed directory remains the
  harness-visible slug.

### 6.7 Persistent state ledger

V1 keeps a persistent local state ledger, but the ledger is not the only authority. It
is a memory and conflict-resolution layer over target-local facts:

- A symlink is safe to mutate when it resolves to an expected managed source.
- A copy is safe to mutate when its marker matches the owner/canonical ID and its digest
  state allows the requested operation.
- A delete still requires target-local proof, even if the ledger says the target was
  once installed by skiller.
- If the ledger is missing or corrupt, `status` and `state repair` rebuild as much as
  possible from manifests, registry roots, markers, legacy ownership adapters, and
  filesystem inspection.

The state file should stay small and human-inspectable: a single JSON file behind
`pkg/state`, not SQLite in v1. Because the ledger is **global** (cross-target) while
per-target locks only serialize one target, two skiller processes writing *different*
targets could still race the same file. So `pkg/state` serializes every
read-modify-write behind one global state lock (`state.json.lock`) and commits with a
temp-file + atomic `rename`, never an in-place rewrite — a crash or concurrent writer
leaves the prior valid ledger intact (and it is rebuildable regardless, per above). The
state lock is held only for short ledger transactions: source/provenance updates finish
and release it before any target-root locks are acquired, and target mutation updates take
it only after the sorted target locks are already held. No code path waits for target locks
while holding `state.json.lock`. The file records the latest known view, not an append-only
event stream:

```text
sources(id, owner, namespace, package_ref, version,
        source_kind, original_spec, canonical_uri, source_key, subdir,
        pinned_ref, requested_checksum, resolved_revision, source_status,
        local_cache_path,
        source_realpath, source_digest, fetched_at, discovered_at, last_seen_at)
skills(id, canonical_id, namespace, name, install_slug, frontmatter_name,
       source_id, description, created_at, updated_at)
installs(id, skill_id, target_kind, target_id, target_path, mode, scope,
         marker_path, installed_digest_at_install, source_digest_at_install,
         status, legacy_adapter, installed_at, updated_at, last_seen_at)
extras(id, source_id, extra_id, target_path, mode, marker_path, installed_at,
       updated_at, last_seen_at)
conflicts(id, target_kind, target_id, effective_name, existing_canonical_id,
          desired_canonical_id, status, resolution, resolved_at, updated_at)
```

State location default order: CLI `--state-dir`, env `SKILLER_STATE_DIR`, platform
state dir (`$XDG_STATE_HOME/skiller`, macOS Application Support, Windows
LocalAppData), then a documented fallback under the user's home. Tests must always use
an explicit temp state dir.

`skiller status --json` merges the ledger with live inspection. Live filesystem facts
win over stale state; stale state is reported as `orphaned` or `not-seen`, not silently
trusted. Mutating commands update the ledger after successful application and record
blocked conflicts before returning.

The first scan classifies every target with an explicit ownership taxonomy:

- `ours-symlink`: symlink realpath points to the desired skiller-managed source.
- `ours-copy`: copy marker is a skiller marker for the same owner/canonical ID.
- `ours-legacy`: a configured legacy adapter recognizes a prior install from an
  adopting tool, such as `.gnit-skill-managed` or an our-ai marker.
- `foreign-known`: a recognized non-skiller installer marker or marketplace/direct-CLI
  install owns the target.
- `foreign-unmanaged`: a target exists but has no recognized ownership proof.
- `satisfied-by-foreign`: an existing local, direct-CLI, marketplace, or otherwise
  foreign install has the desired effective name and matching digest, so it satisfies
  the request without mutation.
- `partially-satisfied`: one or more selected readers are satisfied by an existing
  foreign install, but the requested target group is not fully satisfied without
  creating a duplicate for those readers.
- `absent`: no target exists.

This taxonomy is the coexistence spine. A direct CLI install that already put the same
skill in the desired target should become `satisfied-by-foreign` or an explicit
ledger-only `adopt-existing` resolution, not a duplicate directory under another slug.

**Recognizing ownership is data-driven.** What separates `ours-legacy` from
`foreign-known` is not a hardcoded branch; it is an embedded, extensible marker table
(`data/markers.toml`) that sits beside the harness registry. Each entry is a
`{owner_label, marker_file | ownership_test, class}` where `class ∈ {ours-legacy,
foreign}`:

```toml
# data/markers.toml
[[markers]]
owner_label = "gnit"
marker_file = ".gnit-skill-managed"   # adopting tool's prior install -> adoptable as ours
class = "ours-legacy"

[[markers]]
owner_label = "our-ai"
marker_file = ".our-managed.json"
class = "ours-legacy"

[[markers]]
owner_label = "vercel-skills"
ownership_test = "lockfile"            # owners whose proof is not an in-dir file
class = "foreign"                      # coexist; never mutate without resolution
```

Owners whose proof is not a file inside the skill dir (e.g. clawdapus' global
`~/.claw/skill-installed` plus raw `SKILL.md` copies, or a marketplace lockfile) supply a
named `ownership_test` predicate instead of a `marker_file`. Adding "recognize tool X /
marketplace Y" is then a **data change, not new planner code** — the same data-driven
extensibility as the harness registry. `ours-legacy` entries make a tool's own
pre-skiller installs adoptable; `foreign` entries mark installs we coexist with and never
mutate without explicit conflict resolution. The legacy ownership adapters of §9 are just
the `ours-legacy` rows of this one table.

Foreign occupant decision table:

| Existing target | Desired digest match? | Default plan |
| --- | --- | --- |
| absent (no existing target) | n/a | install (link or copy per mode) |
| owned by skiller/current source | yes | no-op |
| owned by skiller/current source | no, target unmodified | refresh/sync |
| owned by skiller/current source | no, target modified | preserve/block unless forced |
| `ours-legacy` (adopting tool's own prior install) | yes | adopt as ours; no-op or marker upgrade |
| `ours-legacy` | no | adopt, then refresh/replace-as-owned (its lineage is ours) |
| foreign known or unmanaged | yes | `satisfied-by-foreign`, no write, ledger record only |
| foreign known or unmanaged | no | conflict, no write until resolved |
| foreign match in a selected reader's proprietary duplicate root | yes | `partially-satisfied`, no shared write until resolved |

`adopt-existing` never writes a marker into a foreign target in v1. It records that the
foreign install satisfies the desired skill so later runs remain non-destructive and do
not create duplicates. Converting a foreign install into a skiller-owned copy is a
separate explicit replacement action. One trade-off `status` must surface, never hide:
`satisfied-by-foreign` yields update-propagation to the foreign owner — unlike an
`ours-symlink` install, a later source bump does not flow through until the user replaces
the foreign copy — so the two are reported as distinct states, never collapsed into a
single "installed".

**Single ownership truth on `ours-legacy` adoption.** An `ours-legacy` target is the
adopting tool's *own* prior install (its lineage is ours), so when skiller actually
**materializes ownership** of it — the marker-upgrade and replace-as-owned rows of the
table, where a `.skiller-install.json` is written — it also **removes the recognized
legacy marker** (`.gnit-skill-managed`, `.our-managed.json`, …) so exactly one in-dir
ownership marker remains and an old version of the legacy tool can't re-recognize and
clobber the directory. The adopted-from lineage is preserved in the ledger, not by
keeping the stale file. The marker cleanup happens only after the skiller marker or
replacement has been written successfully; a failed cleanup leaves a repairable
dual-marker state, not a half-owned foreign target. This cleanup applies **only** to
`ours-legacy` markers; `foreign` markers are never touched (a no-op `adopt-existing`
writes nothing at all, so it leaves both the directory and any marker untouched).

### 6.8 Namespaces and conflicts

Claude plugins and marketplaces are real prior art: for example, Superpowers is
installable through Claude's official plugin marketplace and its own marketplace. That
means this installer will coexist with skills from unrelated packages, not just our
four tools.

Namespace rules:

- Canonical identity is `namespace:name` and is stored in the manifest, marker, state
  ledger, and JSON plan/status output.
- The namespace is configurable by CLI arg, env (`SKILLER_NAMESPACE`), manifest, or
  local config default.
- The harness-visible name is still flat. Namespace alone does **not** prevent a
  conflict if two sources both expose `name: debugging`.
- By default, installing a different canonical ID with the same effective
  harness-visible name in the same target is **blocked** and recorded in state.
- `--install-slug` / manifest `install_slug` can choose a distinct directory name.
  If the source `SKILL.md` frontmatter name would still collide, **v1 refuses and
  reports** the conflict (no silent rewrite). A copy/overlay mode that writes an
  install-local generated `SKILL.md` with the effective name is **deferred to a later
  version** (see §11): it mutates skill content (digest churn) and is only needed for
  rare cross-namespace name collisions our own four-tool fleet will not hit.

No silent source mutation. Symlink installs cannot rewrite frontmatter.

Conflict resolution modes:

- Default non-interactive mode is `--on-conflict block`: record the conflict and return
  a plan/status result explaining the safe choices.
- `--interactive` or `--on-conflict prompt` opens a TTY prompt when stdin/stdout are
  interactive. Choices include use/adopt existing without mutation, keep existing and
  skip, replace if owned, install under a new slug, force replace, or abort. Unsafe
  choices require explicit confirmation. If `prompt` is requested but no TTY is attached
  (CI, pipes), it does **not** hang or silently pick a default: it falls back to `block`
  and reports the same safe choices for a follow-up non-interactive `--on-conflict` run.
- Automatic modes are explicit args: `--on-conflict skip`,
  `--on-conflict adopt-existing`, `--on-conflict replace-owned`,
  `--on-conflict rename`, and `--on-conflict force-replace`. `rename` requires
  `--install-slug` or a manifest-provided alternate; `force-replace` also requires
  `--force`.
- Renaming is explicit through `--install-slug` or manifest `install_slug`; the resolver
  can suggest a slug but should not silently invent one.
- Resolved decisions are written to the state ledger with the selected resolution and
  target paths, so later `status` can explain why a shape exists.

### 6.9 Source resolution & provenance

A skill can come from anywhere: a local directory, a Git/GitHub repo, a raw web
`SKILL.md`, or an archive. `skiller install <spec>` should "just work" on a pasted URL —
fetch the skill, install it, and remember where it came from so a later `update` can
refresh it — **without any central source registry**. The user-facing contract is a
normalized **SourceSpec**, modeled on the well-trodden Terraform / `go-getter` source
grammar but owned by us (no library is part of the contract).

**SourceSpec grammar.** One string, auto-detected, with explicit overrides for
robustness:

- `./rel`, `/abs`, `file::<path>` — local directory (the default kind).
- `github.com/org/repo`, `org/repo`, `git::https://host/repo.git`, `git@host:org/repo`
  — Git. A host-normalizer maps GitHub/GitLab web URLs (`/tree/<ref>/<path>` or
  `/blob/<ref>/<file>`) to repo + ref + subdir/file.
- `https://host/path/SKILL.md` — a single web file (`http-file`).
- `https://host/path/skill.tar.gz` / `.zip` — a web `archive`.
- Add-ons: `//subdir` selects a skill inside a repo; `?ref=<tag|branch|sha>` pins a
  version; `?checksum=<algo:hex>` pins content.

A forced scheme (`git::`, `http::`, `file::`) always wins over detection. **Traverse-up is
bounded to known hosts**: GitHub/GitLab URLs normalize to repo+ref+subdir; an arbitrary
HTML URL is never guessed into a repo root — it is treated as `http-file` or `archive`
unless a host-normalizer recognizes it. If a spec like `org/repo` also exists as a local
path, the local path wins; use a forced scheme or full Git URL for the remote shorthand.

**Resolution stage.** `pkg/source` turns a spec into a `SourceSnapshot` *before*
observe/plan/apply:

```text
SourceSnapshot{
  source_kind,        // file | git | http-file | http-archive | <future>
  original_spec,      // verbatim, for UX + audit
  canonical_uri,      // normalized identity, for dedupe + update
  source_key,         // stable hash of canonical origin + subdir + requested ref selector
  subdir, pinned_ref, requested_checksum, // selection + requested pins
  resolved_revision,  // git SHA, or http ETag/Last-Modified + content digest
  source_status,      // refreshed | cached | cached-unverified | offline
  local_cache_path,   // materialized working copy
  source_digest, fetched_at,
}
```

The planner and apply consume snapshots, never raw URLs, so apply stays source-agnostic.
The operator's "hash it or store it verbatim" is answered by keeping **all three**
identity layers: `original_spec` (human/audit), `canonical_uri` (dedupe), and `source_key`
(the stable per-source key, used as the cache/ledger key across versions). `source_key`
excludes `resolved_revision` so a floating branch can update in place, but it includes the
requested ref selector when one was supplied, so two branches of the same repo/subdir do
not collide.

**Fetchers are typed; detectors are data.** Each `source_kind` is a typed `Fetcher`
(`file`, `git`, `http-file`, `http-archive`) because each has distinct update and security
semantics. Host shorthands / detectors live in an extensible `data/sources.toml` (beside
`harnesses.toml` and `markers.toml`), so recognizing a new host is a data change;
supporting a genuinely new *kind* is a new `Fetcher`. Anything without a fetcher is an
explicit "unsupported source" error, never a guess.

**Materialization & install mode.** Remote sources fetch into a managed source store keyed
by `source_key`; the normal link/copy install then proceeds from there. Remote installs
**default to copy** (a symlink into a re-fetchable cache is fragile); `link` is opt-in,
warns, and points at a stable per-source materialized dir that `update` refreshes in
place. Local-dir sources keep `link` as the default. Source-store writes use a per-source
cache lock plus staged content and atomic rename, then release that lock before any
target-root locks are acquired.

**Update.** `skiller update [name|--all]` (and `skiller sources refresh`) re-resolves the
recorded source, fetches, and compares `resolved_revision`/digest. A **pinned** ref
(tag/sha/checksum) is frozen until `--latest` or an explicit re-pin; a **floating** ref
(branch / default branch / a web URL) refreshes to the newest revision. Any change then
flows through the **same** conflict/ownership machinery (§6.5–§6.8) — a locally modified
copy is preserved unless `--force`. Apply is never special-cased for remote sources.

**Offline & network failure.** A network/timeout failure while re-resolving a floating
ref must not block an otherwise-applicable install or update. If a materialized snapshot
already exists in the source store, skiller **falls back to the last cached snapshot**,
proceeds, and surfaces a warning that the remote could not be refreshed (the install is
from a possibly-stale cache; `resolved_revision` is marked unverified and Plan/status
surface `source_status: cached-unverified`). A global `--offline` flag skips remote
resolution entirely and uses the cache by contract (reported as `source_status: offline`,
not a warning). If a source has **no** cached snapshot and the network is unavailable (or
`--offline` is set), that one source is an explicit error — skiller never fabricates
content — while other resolvable sources in the same run still proceed. A checksum-pinned
source still verifies the cached snapshot against the requested checksum; mismatch is an
error, not an offline fallback.

**Multi-skill & security.** If a resolved source contains one `SKILL.md`, install it; if
it contains several (`*/SKILL.md`), the plan lists all but requires `--all` or
`--skill/--name` — no surprise bulk install from a repo root. Private sources use the
host's **ambient** git credentials / SSH / `.netrc`; skiller stores no credentials in v1.
Archives extract with a zip-slip path-traversal guard and size/file-count limits. Because
a fetched skill can carry scripts, the first ad-hoc install of an untrusted remote source
asks for confirmation unless `--yes` or a trusted-source policy applies.

## 7. CLI surface

```
skiller plan      [--manifest m.toml | <source-spec> --name …] [--target … | --target-dir …] [--namespace N] --json
skiller install   [<source-spec> | --manifest …] [--ref R] [--subdir S] [--checksum C] [--skill N|--all] [--mode link|copy] [--owner X] [--scope host|runtime] [--namespace N] [--yes] [--json]
skiller update    [name|--all] [--latest] [--json]                  # re-resolve recorded sources; refresh if changed
skiller status    [--manifest …]    [--namespace N] --json          # what's installed, drift, ownership, provenance
skiller sources   list|refresh --json                               # recorded source provenance; refresh = re-resolve+fetch
skiller sync      [--manifest …]    [--namespace N] [--json]         # re-link, refresh digests, prune stale managed installs
skiller uninstall [--manifest …] [--target … | --target-dir …] [--shared] [--all] [--force] [--json]
skiller registry  --json
skiller conflicts list|resolve --json
skiller state     doctor|repair --json
skiller selfupdate [--check]
```

Global flags: `--state-dir DIR`, `--home DIR`, `--project DIR`, `--namespace N`,
`--on-conflict MODE`, `--interactive`, `--offline`, `--json`. `--offline` skips remote
source resolution and uses the cached snapshot store (§6.9).

- Every mutating command supports `plan`/`--dry-run` returning the same JSON `Plan`.
- `uninstall` honors talking-stick's Option-B rule: single-target uninstall leaves the
  shared `~/.agents/skills` and prints a hint; shared removed only by `--shared`/`--all`.
- `install` accepts either a manifest or a single `<source-spec>` (§6.9) — the
  "paste a URL" path — and updates only the selected skills; it does not broadly prune.
- `update` / `sources refresh` re-resolve recorded provenance and re-install only when a
  source's resolved revision changed, through the normal conflict/ownership rules; pinned
  refs need `--latest`. `sources list` reports where each install came from.
- `sync --manifest` updates desired skills and prunes marker-owned skills no longer
  declared by that manifest, preserving foreign and locally modified targets.
- `cleanup-duplicates` should be a distinct command or sub-plan used by integrations to
  remove old proprietary duplicates only when realpath/marker proves ownership.
- Result/status JSON should use a stable taxonomy compatible with existing consumers:
  `installed | updated | removed | skipped | dry-run | failed | not-installed |
  blocked | stale | modified`.

## 8. Distribution & self-update

- Build: **GoReleaser** → static binaries (darwin/linux/windows × amd64/arm64) +
  checksums on GitHub Releases.
- Fetch: `curl … | sh` installer script; also `go install`; vendorable.
- Update: **go-selfupdate** style version check (`skiller selfupdate`); on a new
  release, normal commands print an "update available" notice (Vercel-CLI style).
- Integration per consumer:
  - **talking-stick (Node)**: shell out to the binary (replaces the TS install module);
    or keep a thin TS shim that downloads + execs it.
  - **our-ai / clawdapus (Go)**: `import` `pkg/` directly **or** shell out; clawdapus
    uses runtime target-dir mode (§6.2).
  - **gnit (Rust)**: shell out to the binary.
- The tool can **self-bootstrap**: a consuming tool ships a tiny "ensure installed +
  up to date" step that downloads the right binary on first run ("they offer to
  download and it helps itself up to date").

## 9. Migration from talking-stick

talking-stick's install module is the reference implementation; port, don't rewrite
behavior:

- `harness-model.ts` → `data/harnesses.toml` + `pkg/registry`.
- `skill-install.ts` (target resolution, link/copy, dup cleanup, dedupe) → `pkg/plan`
  + `pkg/install` + `pkg/prune`.
- `install-commands.ts` (`--all`/`--shared`/`agents`, hints, dedup) → `cmd/skiller`.
- `install-migration.ts` / `update-migration.ts` → `pkg/install` migration step (e.g.
  the `~/.agents` move we shipped in 0.4.13).
- Grok hook + instructions-file management stay in talking-stick: hook via `[[extras]]`;
  instructions-file logic remains TS (out of scope for the generic installer).
- Cutover: talking-stick `tt install` calls the binary; keep the TS path until the
  binary reaches behavior parity (validated against talking-stick's existing test
  suite as a conformance oracle).
- Each adopting tool needs a legacy ownership adapter so old installs can be safely
  adopted or cleaned without duplicating planner logic:
  - talking-stick: symlink realpath into bundled source and proprietary duplicate
    paths.
  - gnit: `.gnit-skill-managed`.
  - our-ai: marker file `bundle.MarkerName`, JSON installer in `{our, our-ai}`, plus
    symlink realpath within managed source roots such as `~/.local/share/our*/skills`.
  - clawdapus: `~/.claw/skill-installed` plus raw `SKILL.md` copies.
  These adapters are not bespoke code per tool: they are the `ours-legacy` rows of the
  data-driven `data/markers.toml` table (§6.7), the same mechanism that recognizes
  `foreign` installs for coexistence. Adding or adopting a tool is a data change.
- Gemini transition code stays outside the core. If `our-ai` still has old
  `gemini skills link` behavior at adoption time, replace it with Antigravity/`agents`
  or keep it as a short-lived tool-local migration path that never enters
  `data/harnesses.toml`.

## 10. Testing

- `pkg/plan` pure unit tests (table-driven) — the bulk of correctness lives here.
- `pkg/install` integration tests against a temp `HOME`/`--target-dir`, **with isolated
  env** (the talking-stick lesson: isolate `XDG_CONFIG_HOME`/`HOME` *and* use a temp
  source so realpath checks can't match a real install).
- Idempotence, atomicity (crash-mid-install leaves no partial), link→copy fallback,
  runtime-copy default, conservative-delete, modified-copy preservation,
  dup-prune-preserves-foreign.
- State/status tests: conflict detection by namespace/name, conflict persistence,
  interactive resolver simulation, automatic `--on-conflict` modes, state rebuild from
  markers, orphan detection, duplicate reporting, namespace default precedence, legacy
  marker recognition, and install-slug/frontmatter collision behavior.
- Project/global scope tests: `--global` maps named `agents` to `~/.agents/skills`,
  `--project DIR` maps it to `DIR/.agents/skills`, and `--target-dir` bypasses both.
- Concurrent-run tests: two installers targeting the same skill serialize on the same
  lock; two installers targeting disjoint roots do not unnecessarily block each other;
  multi-target installs acquire locks in sorted order; source-cache/state locks are never
  held while waiting for target locks.
- Conformance: replay talking-stick install scenarios and diff results.

## 11. Roadmap

This roadmap is the convergence target for replacing built-in skill installers across
talking-stick, our-ai, gnit, and clawdapus. The order is intentionally conservative:
prove semantics once, keep adopters thin, and remove legacy code only after each tool
has shipped through a compatibility window.

### 11.1 Operating principles

1. **One semantic core, thin adapters.** Each tool may keep its command names
   (`tt install`, `our skills`, `gnit skills`, `claw skill install`), but install
   planning, ownership checks, shared-target grouping, marker handling, copy/link
   application, and target safety reporting belong in `skiller`.
2. **Plan before apply.** Every adopting tool must be able to show the exact `skiller`
   plan it will apply. A consumer-specific wrapper can change presentation, not the
   underlying action graph.
3. **Coexistence before convenience.** Existing local, marketplace, and direct-CLI
   installs must be detected and either reused/adopted or blocked with a resolver
   choice. Creating duplicate skill directories to avoid deciding is a failure.
4. **Conformance before cutover.** talking-stick's current install tests are the
   reference oracle because they encode the hardest shared/proprietary target behavior.
   gnit and our-ai add legacy-marker and embedded-binary cases. clawdapus adds the
   host/self-skill and runtime-target-dir cases.
5. **Compatibility before deletion.** A tool-local installer is removed only after the
   skiller path has shipped in that tool, passed fixture-home parity, and can adopt or
   safely ignore previous installs.
6. **No registry sprawl in v1.** The registry starts with the verified fleet only:
   `agents`, `claude-code`, `codex`, `antigravity`, `grok`, and `opencode`. Runtime
   directories are explicit target dirs, not fake host harnesses.

### 11.2 Milestone 0 - lock the contract

Deliverables:

- Finalize the manifest schema, marker schema, JSON plan schema, status taxonomy, and
  embedded harness registry fields.
- Finalize the persisted state schema, ownership taxonomy, and conflict-resolution
  policy names.
- Finalize the **SourceSpec** grammar and **SourceSnapshot** / provenance fields
  (`source_kind`, `original_spec`, `canonical_uri`, `source_key`, `resolved_revision`, …)
  so the ledger and planner carry provenance from day one (§6.9), before remote fetchers
  ship.
- Commit fixture examples for one bundled self-skill, one manifest-declared skill, one
  extra file, one runtime target-dir install, one stale copy, one modified copy, and
  one namespace collision.
- Add fixtures for direct/local installs that already occupy the desired target so day
  one adopters can prove they do not create duplicates.
- Define legacy ownership predicates as data/configurable adapters, not hardcoded
  tool branches in `pkg/install`.

Gate:

- `skiller plan --json` can represent all four tools' current install intents without
  writing files.

### 11.3 Milestone 1 - pure planner, registry, and state reads

Build:

- `pkg/registry`, `pkg/manifest`, `pkg/state`, `pkg/observe`, `pkg/status`, pure
  `pkg/plan`, and `pkg/source` with the local `file` resolver (SourceSnapshot plumbing so
  the planner consumes snapshots, not paths/URLs, from day one).
- CLI: `skiller plan --json`, `skiller registry --json`, `skiller status --json`, and
  `skiller conflicts list --json` in read/plan mode.
- Stable lock ids and target paths in the plan output.
- Plan-time conflict detection is pure over an observed `WorldState` (the impure scan
  lives in `pkg/observe`); `pkg/plan` does no I/O. No writes yet.

Tests:

- Fixture-home tests for target resolution, shared-target grouping, namespace collision
  blocking, manifest default precedence, `install_slug`/frontmatter collisions, and
  registry validation.
- State read/rebuild tests for recognized foreign installs, legacy markers, direct CLI
  installs, duplicate prevention, and stale ledger entries.
- Digest-match tests proving unmarked direct CLI installs become
  `satisfied-by-foreign` and do not receive skiller markers.
- Golden JSON plans for talking-stick, our-ai self-skill, our-ai manifest skills, gnit,
  and clawdapus host self-skill.

Gate:

- No mutating command exists yet. All adopters can compare desired behavior by diffing
  `skiller plan --json` against their current dry-run output, including existing direct
  CLI installs that should be reused instead of duplicated.

### 11.3a Milestone 1b - source fetchers & provenance (resolve, no target writes yet)

Build:

- Typed `Fetcher`s in `pkg/source`: `git` (via the host's `git`/SSH so auth is ambient),
  `http-file`, and `http-archive` (zip-slip guard + size/file-count limits).
- `data/sources.toml` host shorthands / detectors; GitHub/GitLab `/tree|/blob` web-URL
  normalizers; `//subdir`, `?ref=`, `?checksum=` grammar.
- Materialized source store keyed by `source_key`; full provenance recorded in the ledger.
- Per-source cache locks and staged atomic cache writes, released before target-root locks
  are acquired.
- Offline behavior: cached-snapshot fallback on network failure for floating refs, the
  `--offline` flag, `source_status` reporting, checksum verification against cached
  content, and `resolved_revision` unverified-marking (§6.9).
- `skiller plan <source-spec> --json` resolves + fetches and shows the would-be install;
  `skiller sources list --json`; multi-skill selectors (`--all`/`--skill`). Still **no
  skill target writes** — cache/state writes are allowed, consistent with the pre-M2
  target-safety rule.

Tests:

- Resolve/normalize: detection vs forced scheme, GitHub/GitLab web URL -> repo+ref+subdir,
  `source_key` stability across revisions, ambiguous-spec errors.
- Fetch against fixtures (no live network): git ref pin/float, http-file ETag, archive
  extraction with path-traversal + size guards.

Gate:

- `skiller plan <spec> --json` resolves local, git, and web sources, fetches into the
  store, and records full provenance. End-to-end `install <spec>` and `update` complete
  once the M2 writer lands.

### 11.4 Milestone 2 - writer, state, and conformance

Build:

- `pkg/install`, `pkg/prune`, `pkg/state` write/repair, `install`, `uninstall`,
  `status`, `sync`, and `cleanup-duplicates`.
- Atomic link/copy application, per-target locks, same-filesystem staging, ownership
  markers, three-way copy digest handling, conservative delete, symlink realpath
  ownership, and link-to-copy fallback.
- Default `--on-conflict block` behavior that records conflicts in state and refuses to
  mutate unsafe targets.
- Legacy ownership adapters for:
  - talking-stick bundled-source symlinks and old proprietary duplicate paths.
  - gnit `.gnit-skill-managed` copies and symlinks to the managed source.
  - our-ai `bundle.MarkerName` markers with installer `{our, our-ai}` and managed
    source roots under `~/.local/share/our*`.
  - clawdapus `~/.claw/skill-installed` as a migration signal for host self-skill
    adoption only.

Tests:

- Replay talking-stick's install/uninstall/update-migration scenarios as a conformance
  suite.
- Add gnit parity fixtures for embedded managed source materialization and marker read
  errors.
- Add our-ai parity fixtures for canonical IDs, manifest install slugs, tool-provided
  skills, and stale-vs-modified copies.
- Add clawdapus fixtures for `~/.claude/skills/clawdapus-cli` and
  `~/.agents/skills/clawdapus-cli` adoption without touching runtime surface markdown.
- Add day-one coexistence fixtures: a skill installed directly by another CLI at the
  desired target is reported as `satisfied-by-foreign` or blocked for explicit
  adoption, never duplicated.
- Prove adoption is ledger-only: `--on-conflict adopt-existing` does not mutate the
  foreign target or write a skiller marker into it.

Gate:

- A full fixture run proves idempotence, no shared-target duplicates, no foreign
  clobber, safe uninstall, and deterministic JSON status across all four tool shapes.

### 11.5 Milestone 3 - conflict resolver UX

Build:

- Interactive resolver for TTY runs: use/adopt existing, keep/skip, replace owned,
  rename with an explicit slug, force replace with confirmation, or abort.
- Non-interactive policies: `block`, `skip`, `adopt-existing`, `replace-owned`,
  `rename`, and `force-replace`.
- `skiller conflicts resolve --json` for CI and wrapper tools that want to present
  their own UI while still using skiller's resolver.

Tests:

- Prompt simulation tests for each interactive branch.
- Non-interactive policy tests proving `our-cli`-style deployment can reuse local/direct
  installs without creating duplicates.
- State tests proving remembered resolutions show up in later `status --json`.

Gate:

- Conflict resolution is complete before any adopter makes skiller the default path.

### 11.6 Milestone 4 - distribution and bootstrap

Build:

- GoReleaser static binaries, checksums, release notes, installer script, and
  `skiller selfupdate`.
- A tiny `ensure-skiller` bootstrap contract for Node, Go, and Rust consumers:
  find existing binary, verify minimum version, download if allowed, otherwise print an
  exact install command.
- A Go library import path for our-ai and clawdapus, plus a stable subprocess JSON
  contract for talking-stick and gnit.

Gate:

- Each consumer can run `skiller --version`, `registry --json`, and a dry-run plan in
  CI without relying on Node, Python, npm, cargo, or a source checkout on the target
  host.

### 11.7 Milestone 5 - first adopters with fallback

Adopt in two passes, because "reference behavior" and "lowest blast radius" are not the
same thing.

1. **talking-stick as the conformance oracle.** Keep the TypeScript path available
   behind an emergency fallback while `tt install --print`, `tt install --all`,
   `tt uninstall`, first-run skill sync, stale MCP cleanup, Grok hook extras, and
   shared `.agents` behavior are replayed through `skiller`. The Grok hook remains an
   `[[extras]]` file placement; instructions-file layering and MCP cleanup stay in
   talking-stick.
2. **gnit as the small subprocess pilot.** Replace `src/skills.rs` install planning and
   writes with `skiller` subprocess calls while preserving the existing CLI surface:
   `gnit skills install|uninstall|list`, `--all`, explicit harnesses, `--copy`,
   `--link`, `--print`, and `--force`. The Rust code should keep only embedded skill
   materialization, bootstrap, and output adaptation.

Gate:

- Both tools ship one release where the `skiller` path is default, legacy behavior can
  still be reached for recovery, and fixture-home parity is documented in release
  verification notes.

### 11.8 Milestone 6 - our-ai cutover

Adopt through the Go library unless subprocess parity proves simpler for release
management.

Scope:

- `our skills self install|uninstall|status`, install-script self-skill setup,
  `doctor --fix`, and quiet self-skill sync.
- Manifest-declared organization skills used by `our setup`, `our sync`,
  `our manifests sync`, `our skills install|sync|purge|status`, and tool-provided
  `skill_install` commands.

Rules:

- Preserve the public manifest schema. our-ai translates its manifest declarations to a
  skiller plan in memory or via a generated temp manifest; operators do not learn a
  second manifest format.
- Keep admin authoring in our-ai (`our admin skills`, `our admin tools`). skiller is the
  materializer, not the manifest authoring UI.
- Existing direct CLI/local installs are day-one inputs. `our setup` and
  `our skills sync` must reuse/adopt/block them explicitly instead of creating duplicate
  skill directories.
- Keep Gemini cleanup/deprecation outside `data/harnesses.toml`. If a live our-ai
  migration still needs Gemini cleanup, it is tool-local transition code.

Gate:

- `our setup`, `our doctor --fix`, and `our skills sync/purge/status --json` return the
  same or stricter statuses than today, with canonical IDs preserved and modified local
  copies protected.

### 11.9 Milestone 7 - clawdapus cutover

Adopt in two lanes:

1. **Host CLI self-skill.** Replace `claw skill install` and `maybeSyncSkill` raw writes
   with skiller-managed installs to Claude Code and the shared `.agents` target. The
   existing `~/.claw/skill-installed` marker is read only as a migration/adoption hint.
2. **Runtime Agent Skill target-dir mode.** Use `--target-dir ... --scope runtime`
   only for driver-declared Agent Skill directory layouts. Do not route flat
   service-surface markdown generation, generated `CLAWDAPUS.md`, service descriptor
   extraction, or pod skill inheritance through skiller in v1.

Gate:

- A clawdapus fixture pod proves runtime target-dir copy mode survives container mount
  boundaries, and existing flat skill files continue to be generated by clawdapus
  unchanged.

### 11.10 Milestone 8 - remove built-in installers

After each adopter has shipped one stable skiller-backed release:

- Delete duplicated planner/writer code from talking-stick, gnit, our-ai, and
  clawdapus.
- Keep only thin command adapters, bootstrap/version checks, tool-local migration code,
  and tool-specific extras that are genuinely outside skiller's domain.
- Update docs to name `skiller` as the shared implementation while retaining each
  tool's user-facing commands.
- Add cross-repo release checks that fail when a tool modifies local skill-install
  semantics instead of updating skiller.

Gate:

- A matrix run across the four repos verifies clean install, repeated no-op install,
  upgrade/sync, uninstall, foreign target preservation, and modified-copy preservation
  on temp homes.

### 11.11 Later extensions

- Prove registry extensibility by adding one new host harness as a data-only change.
- Export a machine-readable registry that upstream tools can consume.
- Export/contribute the marker-scheme table (`data/markers.toml`) so independent
  installers can recognize each other's installs instead of clobbering them.
- Consider upstream contributions to Vercel's `skills` ecosystem: Antigravity support,
  exported static registry data, or a static-binary backend.
- Add content-rewriting overlay mode only if a real cross-namespace frontmatter
  collision requires it.

## 12. Review resolutions (decision log)

- **Link fallback:** host named targets default link and may auto-fallback to copy when
  symlink creation fails; the result must record that fallback. Runtime target-dir
  installs default copy; link requires explicit opt-in and warns about container
  visibility.
- **Project/global:** v1 should ship both `global_dir` and `project_dir` in the registry.
  Default scope is global. `--project <dir>` uses `<dir>/.agents/skills` for the shared
  target. Explicit proprietary harnesses may create their root when selected directly;
  `--all` autodetect should skip missing proprietary roots.
- **Sync/update:** `install` is additive; `sync --manifest` updates desired and prunes
  stale marker-owned manifest entries; `cleanup-duplicates` removes old proprietary
  duplicates only when owned. Modified copies are preserved unless forced.
- **Digest order:** compute desired source digest, inspect target kind, compute current
  target digest excluding marker, compare to marker's installed digest, then decide
  no-op / safe refresh / marker repair / modified-preserve / force replace.
- **Locks:** use lock keys by canonical target root plus a short global plan/apply lock
  for registry/manifest resolution. The JSON plan should include lock ids and target
  paths so dry runs show contention scope. A mutating command acquires its target-root
  write lock(s) **before** `pkg/observe` and holds them through apply, so the planner
  never applies against a stale `WorldState` (§6.5). Multiple target locks are acquired
  in sorted canonical lock-id order. The global JSON ledger has its own `state.json.lock`
  serializing every read-modify-write with a temp-file + atomic `rename`, because the
  ledger is cross-target and per-target locks do not cover two processes writing different
  targets (§6.7). `state.json.lock` and source-cache locks are short-lived and are never
  held while waiting for target locks.
- **Registry export:** embedded TOML remains authoritative. The CLI should expose
  `skiller registry --json` so non-Go consumers can inspect the exact registry used
  by the binary without parsing TOML or importing Go.
- **Host OpenClaw/Hermes:** do not add as host-autodetected named harnesses in v1.
  Clawdapus uses runtime target-dir mode. Add host profiles only after verifying actual
  runner discovery behavior outside Claw.
- **State:** add a persisted JSON ledger in v1 because day-one coexistence requires
  remembering observed local/direct-CLI installs, conflicts, and chosen resolutions.
  Markers and symlink realpaths remain authoritative for destructive safety; the ledger
  is authoritative for explaining coexistence and avoiding duplicate installs.
- **Namespace policy:** ship canonical namespace support in v1. Conflict prevention is
  blocking/reporting by default; physical renaming is explicit through `install_slug`
  while content-rewriting overlay mode is deferred.
- **Gemini removal:** remove Gemini from the registry and v1 supported harnesses.
  Google cutoff is June 18, 2026 for consumer requests. Tool-local cleanup of old
  Gemini installs is allowed; new shared installer support is not.

**Convergence review 2 (claude) resolutions:**

- **Observe / plan / apply:** lift filesystem reads out of the planner. `pkg/observe`
  produces a `WorldState`; `pkg/plan` is pure over it, so the §6.7 decision table is
  table-testable. Resolves the prior "pure planner that nonetheless scans the FS"
  inconsistency.
- **Data-driven ownership:** unify legacy adapters and foreign-install detection into one
  extensible `data/markers.toml` (`class ∈ {ours-legacy, foreign}`). Recognizing a new
  tool or marketplace is a data change, not planner code.
- **No-duplicate survey scope:** the cross-root duplicate survey covers the roots read by
  the in-scope harnesses (each one's shared + proprietary roots), not every root on disk.
- **Decision-table completeness:** `absent`, `ours-legacy`, and `partially-satisfied` are
  explicit rows; `ours-legacy` adopts as ours rather than blocking as foreign, so a tool
  can replace its own pre-skiller installs.
- **Prompt fallback:** `--on-conflict prompt` with no TTY falls back to `block` + report;
  it never hangs or guesses a default.

**Convergence review 3 (source resolution) resolutions:**

- **Install from anywhere, no source registry:** a normalized `SourceSpec`
  (Terraform/`go-getter` grammar, but our contract — not the library) covers local / git
  / GitHub / web file / archive, with `//subdir` + `?ref=`. Resolution is a
  `pkg/source` stage producing a `SourceSnapshot` before observe/plan/apply.
- **Provenance, three layers:** persist `original_spec` (verbatim/audit), `canonical_uri`
  (dedupe/update), and `source_key` (stable hash of canonical origin + subdir + requested
  ref selector, excluding resolved revision), plus `resolved_revision` for update checks.
  Source identity stays separate from skill identity.
- **Bounded traverse-up:** only known-host normalizers (GitHub/GitLab) map deep URLs to
  repo+ref+subdir; arbitrary HTML is never guessed into a repo root.
- **Detectors = data, fetchers = typed code:** host shorthands live in `data/sources.toml`;
  each `source_kind` is a typed `Fetcher` with its own security/update semantics.
- **Remote = copy by default; updates reuse the machinery:** `update` / `sources refresh`
  re-resolve and re-install only on change; pinned refs need `--latest`; apply is never
  special-cased for remote. Ambient git/SSH/netrc auth; zip-slip + size guards;
  first-install trust confirmation unless `--yes`.
- **Schema day-one, fetchers phased:** SourceSpec + provenance fields land in M0/M1; remote
  fetchers ship in M1b, before the writer.

**Convergence review 4 (external review — Gemini) resolutions:**

- **Lock before observe (TOCTOU):** a mutating command takes its target-root write
  lock(s) before `pkg/observe`, not just at apply, so the plan can't be built on a
  `WorldState` a concurrent process invalidates before the write (§6.5, §12 Locks).
  Multiple target locks are acquired in sorted canonical order, and state/cache locks are
  never held while waiting for target locks. Read-only commands stay target-lock-free.
- **Ledger serialization:** the global JSON ledger is guarded by one `state.json.lock`
  plus temp-file + atomic `rename`, since per-target locks don't stop two processes
  writing different targets from racing the one cross-target file (§6.7).
- **Offline / network-failure fallback:** a floating-ref refresh that fails on the
  network falls back to the last cached snapshot with a warning (`resolved_revision`
  unverified and `source_status: cached-unverified`); `--offline` skips remote resolution
  by contract and reports `source_status: offline`; no cache + offline is an explicit
  per-source error, never fabricated content. Checksum-pinned sources still verify cached
  content (§6.9, lands with the M1b fetchers).
- **Single ownership truth on adoption:** materializing ownership of an `ours-legacy`
  target removes the recognized legacy marker and writes one `.skiller-install.json`,
  recording lineage in the ledger. Cleanup happens only after successful skiller
  marker/replacement write; `foreign` markers are never touched (§6.7, §9).
- **Accepted as-is (no doc change):** Gemini's endorsement of observe/plan/apply purity,
  data-driven `harnesses`/`markers`/`sources.toml`, explicit runtime target modes, the
  Gemini-CLI removal, and the M0→M5 milestone order — all already in the design.

## 13. Naming (decided)

**Name: `skiller`.** Binary `skiller`; suggested short shell alias `skl`. Module path
`github.com/mostlydev/skiller`.

Rationale: the descriptive `skill-*` namespace is saturated and collision-prone —
`skillport` (gotalab/skillport, ~389★, a CLI+MCP skill manager with nearly identical
positioning), `skilljack` (olaservo/skilljack-mcp), `skillpm`, `skillkit`, `skillz`,
`skillshare`, `skills-manager`, `skills-npm`, `askill`, and `gh skill` are all taken. A
coined name sidesteps the lot. `skiller` follows the canonical software-tool naming
convention — the agent-noun `-er`/`-or` suffix of compiler, linker, loader, assembler,
debugger, profiler, builder, **installer** — so it reads as a real tool: "the thing that
skills your agents." It is unclaimed in the Agent Skills ecosystem, short, brandable, and
fits the sibling menagerie (clawdapus, gnit, microclaw).

Runners-up: `skillwire` (Shadowrun "skillwires" literally install skill chips — clean
cyberpunk metaphor, also free) and `skillington`. Rejected: `skilljack` / `skilljack-cli`
(collides with skilljack-mcp; the `-cli` suffix implies an affiliation that does not
exist) and `skillport` (taken; near-identical tool).
