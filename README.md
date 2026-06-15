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

**Status:** M4 distribution/bootstrap machinery is implemented and locally validated.
The repository has the planner, writer, conflict resolver, version JSON contract,
GoReleaser config, installer script, `selfupdate`, reference bootstrappers, and the
experimental Go facade. No real tag/release has been cut yet; that remains an operator
action. Read the design:
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

## CLI

```
skiller --version
skiller version --json
skiller registry --json
skiller plan      --manifest skiller.toml --json
skiller install   --manifest skiller.toml --json
skiller status    --json [--manifest skiller.toml]
skiller conflicts list    --json [--manifest skiller.toml]
skiller conflicts resolve --manifest skiller.toml --json [--resolution ID=POLICY]
skiller sync      --manifest skiller.toml --json
skiller uninstall --manifest skiller.toml --json [--shared|--all]
skiller cleanup-duplicates --manifest skiller.toml --json
skiller state repair --manifest skiller.toml --json
skiller selfupdate [--check] [--dry-run] [--json]
```

It also supports an explicit `--target-dir … --scope runtime` mode for materialized /
containerized runtimes (e.g. clawdapus mounting into OpenClaw or Hermes).

## Distribution And Bootstrap

Release builds are configured through `.goreleaser.yaml` for static darwin, linux, and
windows binaries on amd64/arm64, plus `checksums.txt`.

Installer script:

```sh
curl -fsSL https://raw.githubusercontent.com/mostlydev/skiller/master/scripts/install.sh | sh
```

Reference `ensure-skiller` implementations live under `bootstrap/{node,go,rust}`. They
verify an existing binary by running `skiller version --json` and print the installer
command by default; downloading is opt-in only.

Go consumers can use `github.com/mostlydev/skiller/pkg/skiller`, which is intentionally
experimental until the first Go adopter cutover. Node and Rust consumers should treat the
subprocess JSON contract as the stable M4 surface.

## Name

The agent-noun `-er` suffix is the canonical software-tool convention (compiler, linker,
loader, **installer**): `skiller` is "the thing that skills your agents." See §13 of the
design doc for the rationale and the naming alternatives considered.

## License

MIT — see [LICENSE](LICENSE).
