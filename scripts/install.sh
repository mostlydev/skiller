#!/bin/sh
set -eu

repo="mostlydev/skiller"
version="latest"
bin_dir="${HOME:-}/.local/bin"
base_url="${SKILLER_RELEASE_BASE_URL:-}"

usage() {
  cat <<'USAGE'
Usage: install.sh [--version VERSION] [--repo OWNER/REPO] [--bin-dir DIR] [--base-url URL]

Downloads a skiller release archive, verifies its SHA-256 checksum from checksums.txt,
and installs the skiller binary.

Options:
  --version VERSION  Release tag to install. Defaults to latest.
  --repo OWNER/REPO  GitHub repository. Defaults to mostlydev/skiller.
  --bin-dir DIR      Install directory. Defaults to ~/.local/bin.
  --base-url URL     Override release asset base URL. Intended for tests and forks.
  --help             Show this help.

Security note: checksums.txt is unsigned in M4. Verification detects corruption and
unexpected artifact changes, but it is not a signature and does not protect against a
compromised release account.
USAGE
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || { echo "--version requires a value" >&2; exit 2; }
      version=$2
      shift 2
      ;;
    --repo)
      [ "$#" -ge 2 ] || { echo "--repo requires a value" >&2; exit 2; }
      repo=$2
      shift 2
      ;;
    --bin-dir)
      [ "$#" -ge 2 ] || { echo "--bin-dir requires a value" >&2; exit 2; }
      bin_dir=$2
      shift 2
      ;;
    --base-url)
      [ "$#" -ge 2 ] || { echo "--base-url requires a value" >&2; exit 2; }
      base_url=$2
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

detect_os() {
  case "$(uname -s)" in
    Darwin) echo "darwin" ;;
    Linux) echo "linux" ;;
    MINGW*|MSYS*|CYGWIN*|Windows_NT) echo "windows" ;;
    *)
      echo "unsupported OS: $(uname -s)" >&2
      exit 2
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)
      echo "unsupported architecture: $(uname -m)" >&2
      exit 2
      ;;
  esac
}

asset_base_url() {
  if [ -n "$base_url" ]; then
    echo "$base_url"
    return
  fi
  if [ "$version" = "latest" ]; then
    echo "https://github.com/$repo/releases/latest/download"
  else
    echo "https://github.com/$repo/releases/download/$version"
  fi
}

join_url() {
  case "$1" in
    file://*) printf '%s/%s\n' "${1%/}" "$2" ;;
    http://*|https://*) printf '%s/%s\n' "${1%/}" "$2" ;;
    *) printf '%s/%s\n' "${1%/}" "$2" ;;
  esac
}

fetch() {
  fetch_url=$1
  fetch_dest=$2
  case "$fetch_url" in
    http://*|https://*)
      command -v curl >/dev/null 2>&1 || { echo "curl is required for downloads" >&2; exit 2; }
      curl -fsSL "$fetch_url" -o "$fetch_dest"
      ;;
    file://*)
      cp "${fetch_url#file://}" "$fetch_dest"
      ;;
    *)
      cp "$fetch_url" "$fetch_dest"
      ;;
  esac
}

sha256_file() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  echo "shasum or sha256sum is required for checksum verification" >&2
  exit 2
}

select_archive() {
  checksums=$1
  target_os=$2
  target_arch=$3
  if [ "$target_os" = "windows" ]; then
    suffix="_${target_os}_${target_arch}.zip"
  else
    suffix="_${target_os}_${target_arch}.tar.gz"
  fi
  awk -v suffix="$suffix" '
    NF >= 2 && index($2, "skiller_") == 1 {
      start = length($2) - length(suffix) + 1
      if (start > 0 && substr($2, start) == suffix) {
        print $2
        exit
      }
    }
  ' "$checksums"
}

checksum_for() {
  checksums=$1
  archive=$2
  awk -v archive="$archive" 'NF >= 2 && $2 == archive { print $1; exit }' "$checksums"
}

extract_archive() {
  archive_path=$1
  extract_dir=$2
  case "$archive_path" in
    *.tar.gz)
      tar -xzf "$archive_path" -C "$extract_dir"
      ;;
    *.zip)
      command -v unzip >/dev/null 2>&1 || { echo "unzip is required for Windows archives" >&2; exit 2; }
      unzip -q "$archive_path" -d "$extract_dir"
      ;;
    *)
      echo "unsupported archive format: $archive_path" >&2
      exit 2
      ;;
  esac
}

find_binary() {
  find_dir=$1
  find "$find_dir" -type f \( -name skiller -o -name skiller.exe \) | head -n 1
}

path_contains() {
  path_dir=$1
  case ":${PATH:-}:" in
    *":$path_dir:"*) return 0 ;;
    *) return 1 ;;
  esac
}

target_os=$(detect_os)
target_arch=$(detect_arch)
asset_base=$(asset_base_url)
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT HUP INT TERM

checksums_path="$tmpdir/checksums.txt"
fetch "$(join_url "$asset_base" "checksums.txt")" "$checksums_path"

archive_name=$(select_archive "$checksums_path" "$target_os" "$target_arch")
if [ -z "$archive_name" ]; then
  echo "no skiller archive found for $target_os/$target_arch in checksums.txt" >&2
  exit 1
fi

expected_checksum=$(checksum_for "$checksums_path" "$archive_name")
if [ -z "$expected_checksum" ]; then
  echo "no checksum found for $archive_name" >&2
  exit 1
fi

archive_path="$tmpdir/$archive_name"
fetch "$(join_url "$asset_base" "$archive_name")" "$archive_path"
actual_checksum=$(sha256_file "$archive_path")
if [ "$actual_checksum" != "$expected_checksum" ]; then
  echo "checksum mismatch for $archive_name" >&2
  echo "expected: $expected_checksum" >&2
  echo "actual:   $actual_checksum" >&2
  exit 1
fi

extract_dir="$tmpdir/extract"
mkdir -p "$extract_dir"
extract_archive "$archive_path" "$extract_dir"
binary_path=$(find_binary "$extract_dir")
if [ -z "$binary_path" ]; then
  echo "archive did not contain skiller binary" >&2
  exit 1
fi

mkdir -p "$bin_dir"
install_path="$bin_dir/skiller"
if [ "$target_os" = "windows" ]; then
  install_path="$bin_dir/skiller.exe"
fi
cp "$binary_path" "$install_path"
chmod 0755 "$install_path"

echo "Installed skiller to $install_path"
if ! path_contains "$bin_dir"; then
  echo "Add $bin_dir to PATH to run skiller without an absolute path."
fi
