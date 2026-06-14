# skiller

> Reliable Agent Skill installer for a polyglot fleet of AI tools.

`skiller` installs [Agent Skills](https://agentskills.io) (`SKILL.md` folders) into AI
coding-agent harnesses — the shared `~/.agents/skills` convention plus per-agent
proprietary directories — with **idempotent, atomic** installs, symlink/copy modes,
duplicate cleanup, drift detection (three-way digests), and safe uninstall that never
deletes content it doesn't own.

It ships as a **single static Go binary** (plus an importable Go library core) so that
Go, Rust, and Node tools can all use it **without dragging a foreign runtime onto the
host**.

**Status:** design phase — nothing is implemented yet. Read the design:
[`docs/plans/2026-06-14-skiller-design.md`](docs/plans/2026-06-14-skiller-design.md).

## Why this exists

Extracted and generalized from
[`talking-stick`](https://github.com/mostlydev/talking-stick)'s install module so the
same install semantics can be shared by **talking-stick** (Node), **our-ai** (Go),
**clawdapus** (Go), and **gnit** (Rust).

Existing tools — [`vercel-labs/skills`](https://github.com/vercel-labs/skills) (Node) and
[`gotalab/skillport`](https://github.com/gotalab/skillport) (Python) — prove the problem
and the `SKILL.md` format, but each requires its own interpreter, so neither can be the
shared dependency for a Go/Rust/Node fleet. `skiller` adopts the same open format and
stays runtime-free.

## Planned CLI

```
skiller plan      --manifest skiller.toml --json      # dry-run; same plan as install
skiller install   --manifest skiller.toml             # idempotent, atomic
skiller status    --json                              # installed / drift / ownership
skiller sync      --manifest skiller.toml             # re-link, refresh, prune stale
skiller uninstall --manifest skiller.toml [--shared|--all]
skiller registry  --json                              # the harness registry it uses
skiller selfupdate
```

It also supports an explicit `--target-dir … --scope runtime` mode for materialized /
containerized runtimes (e.g. clawdapus mounting into OpenClaw or Hermes).

## Name

The agent-noun `-er` suffix is the canonical software-tool convention (compiler, linker,
loader, **installer**): `skiller` is "the thing that skills your agents." See §13 of the
design doc for the rationale and the naming alternatives considered.

## License

MIT — see [LICENSE](LICENSE).
