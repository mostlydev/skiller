# ensure-skiller bootstrap contract

These reference implementations are intentionally small and vendorable. They verify that
an acceptable `skiller` binary already exists and print the exact install command when it
does not. The default path does not touch the network.

## Algorithm

1. Find a binary:
   - `SKILLER_BIN`, when set.
   - `skiller` on `PATH`.
   - `~/.local/bin/skiller`.
2. Run `skiller version --json`.
3. Parse the `version` field and compare it to the required minimum version.
4. If the binary is missing, invalid, or too old, print the install command and fail.
5. Download/install only when explicitly allowed by `--allow-download` or
   `SKILLER_BOOTSTRAP_ALLOW_DOWNLOAD=1`.

## Exit Codes

- `0`: acceptable `skiller` found.
- `10`: no `skiller` binary found.
- `11`: found `skiller`, but its version is too old.
- `12`: `skiller version --json` returned invalid JSON or an invalid version.
- `13`: `skiller version --json` failed.
- `20`: explicit install/download command failed.

## Configuration

- `--min-version VERSION` or `SKILLER_MIN_VERSION` selects the minimum version.
- `--allow-download` or `SKILLER_BOOTSTRAP_ALLOW_DOWNLOAD=1` permits running the install
  command.
- `--install-command COMMAND` or `SKILLER_BOOTSTRAP_INSTALL_COMMAND` overrides the command
  printed and run by the explicit-download path.

Default install command:

```sh
curl -fsSL https://raw.githubusercontent.com/mostlydev/skiller/master/scripts/install.sh | sh
```

The installer script lands later in M4; until then consumers should override the install
command or use these references in verify-and-print mode.
