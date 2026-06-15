# skiller <version>

Date: <YYYY-MM-DD>

## Summary

- Static `skiller` binaries for darwin, linux, and windows on amd64/arm64.
- Archives include `README.md`, `LICENSE`, and release notes.
- `checksums.txt` contains SHA-256 checksums for release artifacts.

## Compatibility

- No Node, Python, npm, cargo, or source checkout is required on the target host.
- Supported bootstrap gate: `skiller --version`, `skiller version --json`,
  `skiller registry --json`, and `skiller plan --manifest <path> --json`.
- `skiller version --json` follows `schema/version.schema.json`.

## Verification

- `go test ./... -count=1`
- `go vet ./...`
- `git diff --check`
- `goreleaser check`
- `goreleaser release --snapshot --clean`
- Static smoke: `CGO_ENABLED=0 go build ./cmd/skiller` plus `--version`,
  `version --json`, `registry --json`, and fixture `plan --json`.

## Security Note

`checksums.txt` is unsigned in M4. Checksum verification detects corruption and
unexpected artifact changes, but it is not a signature and does not protect against a
compromised release account. Signing is deferred to a later milestone.
