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
- A local install database that records provenance, namespaces, targets, digests, and
  conflict decisions across all sources this tool has installed.
- Reliable, **idempotent**, **atomic** installs; safe uninstall that never deletes
  foreign content.
- Two target modes: **named harness targets** (autodetected) and **explicit
  `--target-dir` runtime profiles** (for materialized containers).
- `--json` dry-run (`plan`) for every mutating command.
- Self-update + easy bootstrap.

**Non-goals (v1)**
- Not modeling all 70 agents — only our verified fleet (§6.1).
- Not modeling Hermes/OpenClaw as host-autodetected harnesses — they are runtime
  target-dir profiles (§6.2).
- No skill *authoring*/registry/discovery marketplace (that's Vercel's `skills find`).
- No project-committed scope work beyond what the manifest needs (global scope first;
  `project_dir` is in the registry data but lower priority).

## 5. Architecture

```
skiller/                 (new repo)
├── go.mod
├── pkg/                     library core — the contract for Go consumers
│   ├── registry/            embedded harness registry (go:embed data + typed loader)
│   ├── manifest/            TOML manifest parse/validate
│   ├── plan/                resolve manifest+registry+mode -> install Plan (pure, testable)
│   ├── install/             apply Plan: link/copy, atomic, idempotent, marker write
│   ├── prune/               duplicate cleanup + safe uninstall (marker/realpath gated)
│   ├── state/               local install DB: provenance, namespaces, conflicts
│   └── selfupdate/          version check + update
├── cmd/skiller/         thin CLI over pkg/ (cobra/std flag)
├── internal/                fsutil (atomic rename, symlink+fallback), digest, lock
├── data/harnesses.toml      embedded registry (single source of truth)
└── .goreleaser.yaml         multi-platform static builds + checksums
```

- **`pkg/plan` is pure**: `(manifest, registry, options) -> Plan{actions[]}` with no
  filesystem writes. This is what makes dry-run trivial and the logic unit-testable —
  the same separation that made talking-stick's `planSkillInstall` reviewable.
- **`pkg/install` applies** a `Plan` and is the only writer. Idempotence and atomicity
  live here.
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

`kind ∈ {shared, proprietary}` in v1. Loader is typed; unknown fields are a
validation error so the data and code can't silently drift (mirrors the talking-stick
rule that derived harness lists must not drift). The real registry needs a few more
fields than the sketch: aliases/readers, global and project dirs, env-home overrides,
detect dirs, duplicate-cleanup dirs, default mode, and whether the target can carry
extras. These remain data-only and are covered by fixture-`HOME` tests.

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

### 6.3 Skill manifest (TOML)

A consuming tool ships a manifest; the installer reads it. (Drafted by Codex.)

```toml
schema = "skiller-install.v1"
owner = "talking-stick"       # installer owner / package owner
namespace = "mostlydev"       # canonical namespace for conflict tracking
version = "0.4.14"
default_mode = "link"          # link | copy

[[skills]]
name = "talking-stick"
canonical_id = "mostlydev:talking-stick"   # optional stable owner:name identity
install_slug = "talking-stick"             # harness-visible directory/effective name
source = "./skills/talking-stick"
targets = ["agents", "claude-code"]   # agents == canonical shared ~/.agents/skills
mode = "link"

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
config default, then `owner`. `install_slug` defaults to `name`.

`[[extras]]` keeps tool-specific *data* (talking-stick's Grok hook) in the generic
installer while tool-specific *logic* (instructions-file harness extraction, MCP
cleanup) stays in the owning tool. Directory extras can use the same in-directory
marker as skill copies. Single-file extras need a sidecar marker next to the target
(for example `talking-stick-session.json.skiller-install.json`) so uninstall and
sync remain safe without depending on a global database.

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
- **Duplicate cleanup**: port talking-stick's symlink-only prune — remove only symlinks
  that resolve to the managed source; preserve foreign symlinks and unmanaged copies.
- **Idempotence**: re-running install with the same inputs is a no-op (quiet), matching
  talking-stick's "quiet no-op runs."
- **Atomicity**: stage in a temp dir on the same filesystem, then atomic `rename`;
  acquire a per-target lock to serialize concurrent runs. Replacing a non-empty
  existing directory uses a staged replacement path and a best-effort rollback path,
  never an in-place partial copy.

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

### 6.7 Local install database

Target markers answer "is this path safe to touch?" The local DB answers the broader
questions markers cannot answer:

- Which sources/packages installed a skill with this name?
- Which target paths belong to the same canonical skill?
- Does a new install collide with an existing install from another namespace?
- Which installed entries are duplicates, stale, locally modified, or orphaned?
- What legacy marker/adoption rule created this entry?

The DB is a local ledger, not the only authority. Deletes still require a target marker
or symlink realpath proof; if the DB is missing or corrupt, `status --repair-db` can
rebuild from markers and known target roots.

Suggested schema:

```text
sources(id, owner, namespace, package_ref, version, source_uri, source_realpath,
        source_digest, discovered_at)
skills(id, canonical_id, namespace, name, install_slug, frontmatter_name,
       source_id, description, created_at, updated_at)
installs(id, skill_id, target_kind, target_id, target_path, mode, scope,
         marker_path, installed_digest_at_install, source_digest_at_install,
         installed_at, updated_at, status)
extras(id, source_id, extra_id, target_path, mode, marker_path, installed_at)
conflicts(id, target_kind, target_id, effective_name, canonical_ids, resolution,
          created_at, resolved_at)
```

Backend (v1, decided): a **single JSON ledger file** behind `pkg/state` — not SQLite.
The dataset is tiny (a handful of skills × targets per machine), the ledger must already
be rebuildable from on-disk markers, and a JSON file keeps the binary trivially static
(no CGO `mattn/go-sqlite3`, no `modernc` driver) and human-inspectable. Revisit an
embedded SQL store only if scale ever demands it. The CLI, not direct ledger access, is
the public interface: `skiller status --json`, `skiller registry --json`, and a future
`skiller db doctor|repair`.

State location default order: CLI `--state-dir`, env `SKILLER_STATE_DIR`, platform
state dir (`$XDG_STATE_HOME/skiller`, macOS Application Support, Windows
LocalAppData), then a documented fallback under the user's home. Tests must always use
an explicit temp state dir.

### 6.8 Namespaces and conflicts

Claude plugins and marketplaces are real prior art: for example, Superpowers is
installable through Claude's official plugin marketplace and its own marketplace. That
means this installer will coexist with skills from unrelated packages, not just our
four tools.

Namespace rules:

- Canonical identity is `namespace:name` and is stored in the manifest, marker, and DB.
- The namespace is configurable by CLI arg, env (`SKILLER_NAMESPACE`), manifest, or
  local config default.
- The harness-visible name is still flat. Namespace alone does **not** prevent a
  conflict if two sources both expose `name: debugging`.
- By default, installing a different canonical ID with the same effective
  harness-visible name in the same target is **blocked** and recorded as a conflict.
- `--allow-conflict` may install only when the harness has deterministic shadowing and
  the plan reports exactly which entry wins.
- `--install-slug` / manifest `install_slug` can choose a distinct directory name.
  If the source `SKILL.md` frontmatter name would still collide, **v1 refuses and
  reports** the conflict (no silent rewrite). A copy/overlay mode that writes an
  install-local generated `SKILL.md` with the effective name is **deferred to a later
  version** (see §11): it mutates skill content (digest churn) and is only needed for
  rare cross-namespace name collisions our own four-tool fleet will not hit.

No silent source mutation. Symlink installs cannot rewrite frontmatter.

## 7. CLI surface

```
skiller plan      [--manifest m.toml | --source … --name …] [--target … | --target-dir …] [--namespace N] --json
skiller install   …same selectors…  [--mode link|copy] [--owner X] [--scope host|runtime] [--namespace N] [--yes] [--json]
skiller status    [--manifest …]    [--namespace N] --json          # what's installed, drift, ownership
skiller sync      [--manifest …]    [--namespace N] [--json]         # re-link, refresh digests, prune stale managed installs
skiller uninstall [--manifest …] [--target … | --target-dir …] [--shared] [--all] [--force] [--json]
skiller registry  --json
skiller db        doctor|repair      --json
skiller selfupdate [--check]
```

Global flags: `--state-dir DIR`, `--home DIR`, `--project DIR`, `--namespace N`,
`--json`.

- Every mutating command supports `plan`/`--dry-run` returning the same JSON `Plan`.
- `uninstall` honors talking-stick's Option-B rule: single-target uninstall leaves the
  shared `~/.agents/skills` and prints a hint; shared removed only by `--shared`/`--all`.
- `install` updates selected desired skills only; it does not broadly prune.
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
  The core should expose `legacy_ownership_predicates` / `{marker_filename,
  ownership_test}` hooks rather than baking every old marker into the shared registry.
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
- Local DB tests: conflict detection by namespace/name, DB rebuild from markers,
  orphan detection, duplicate reporting, namespace default precedence, and
  install-slug/frontmatter collision behavior.
- Project/global scope tests: `--global` maps named `agents` to `~/.agents/skills`,
  `--project DIR` maps it to `DIR/.agents/skills`, and `--target-dir` bypasses both.
- Concurrent-run tests: two installers targeting the same skill serialize on the same
  lock; two installers targeting disjoint roots do not unnecessarily block each other.
- Conformance: replay talking-stick install scenarios and diff results.

## 11. Phased plan

1. **M1 — core**: repo, `pkg/registry` + `data/harnesses.toml`, `pkg/manifest`,
   `pkg/state`, `pkg/plan`, `plan --json`, `registry --json`, DB initialization and
   dry-run conflict detection. No target writes yet.
2. **M2 — install/uninstall**: `pkg/install` (link/copy, atomic, idempotent, marker),
   `pkg/prune`, CLI `install`/`uninstall`/`status`/`sync`. Full tests.
3. **M3 — distribution**: GoReleaser, selfupdate, installer script, bootstrap snippet.
4. **M4 — adopt in talking-stick** behind parity tests; then our-ai, gnit, clawdapus
   (runtime mode).
5. **M5 (later)**: extension points proven by adding an agent via data-only change;
   optional upstream contributions to Vercel (Antigravity, static binary, exported
   registry).

## 12. Codex review resolutions

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
  paths so dry runs show contention scope.
- **Registry export:** embedded TOML remains authoritative. The CLI should expose
  `skiller registry --json` so non-Go consumers can inspect the exact registry used
  by the binary without parsing TOML or importing Go.
- **Host OpenClaw/Hermes:** do not add as host-autodetected named harnesses in v1.
  Clawdapus uses runtime target-dir mode. Add host profiles only after verifying actual
  runner discovery behavior outside Claw.
- **Local DB:** add `pkg/state` in M1, even if M1 only writes the DB during `plan` tests
  and no-op dry runs. Provenance/conflict tracking is part of the core contract, not a
  later feature.
- **Namespace policy:** ship canonical namespace support in v1. Conflict prevention is
  blocking/reporting by default; physical renaming is explicit through `install_slug`
  and may force copy/overlay mode.
- **Gemini removal:** remove Gemini from the registry and v1 supported harnesses.
  Google cutoff is June 18, 2026 for consumer requests. Tool-local cleanup of old
  Gemini installs is allowed; new shared installer support is not.

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
