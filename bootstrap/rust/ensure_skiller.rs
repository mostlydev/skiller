use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::{Command, exit};

const EXIT_MISSING: i32 = 10;
const EXIT_TOO_OLD: i32 = 11;
const EXIT_INVALID: i32 = 12;
const EXIT_EXEC: i32 = 13;
const EXIT_INSTALL: i32 = 20;
const DEFAULT_MIN_VERSION: &str = "0.0.0";
const DEFAULT_INSTALL_COMMAND: &str =
    "curl -fsSL https://raw.githubusercontent.com/mostlydev/skiller/main/scripts/install.sh | sh";

struct Options {
    min_version: String,
    allow_download: bool,
    install_command: String,
}

struct EnsureResult {
    ok: bool,
    path: String,
    version: String,
    code: i32,
    message: String,
}

fn main() {
    let opts = parse_args();
    let result = ensure_skiller(&opts);
    if result.ok {
        println!("{} {}", result.path, result.version);
        return;
    }
    if opts.allow_download {
        if !run_install(&opts.install_command) {
            eprintln!("skiller install command failed: {}", opts.install_command);
            exit(EXIT_INSTALL);
        }
        let retry = ensure_skiller(&Options {
            min_version: opts.min_version.clone(),
            allow_download: false,
            install_command: opts.install_command.clone(),
        });
        if retry.ok {
            println!("{} {}", retry.path, retry.version);
            return;
        }
        eprintln!("{}", retry.message);
        eprintln!("Install command: {}", opts.install_command);
        exit(retry.code);
    }
    eprintln!("{}", result.message);
    eprintln!("Install command: {}", opts.install_command);
    exit(result.code);
}

fn parse_args() -> Options {
    let mut opts = Options {
        min_version: env::var("SKILLER_MIN_VERSION").unwrap_or_else(|_| DEFAULT_MIN_VERSION.to_string()),
        allow_download: env::var("SKILLER_BOOTSTRAP_ALLOW_DOWNLOAD").unwrap_or_default() == "1",
        install_command: env::var("SKILLER_BOOTSTRAP_INSTALL_COMMAND")
            .unwrap_or_else(|_| DEFAULT_INSTALL_COMMAND.to_string()),
    };
    let args: Vec<String> = env::args().skip(1).collect();
    let mut i = 0;
    while i < args.len() {
        match args[i].as_str() {
            "--min-version" => {
                i += 1;
                opts.min_version = args.get(i).cloned().unwrap_or_default();
            }
            "--allow-download" => opts.allow_download = true,
            "--install-command" => {
                i += 1;
                opts.install_command = args.get(i).cloned().unwrap_or_default();
            }
            other => {
                eprintln!("unknown argument {}", other);
                exit(EXIT_INVALID);
            }
        }
        i += 1;
    }
    opts
}

fn ensure_skiller(opts: &Options) -> EnsureResult {
    let binary = find_binary();
    if binary.is_none() {
        return fail(EXIT_MISSING, "skiller binary not found");
    }
    let binary = binary.unwrap();
    let version = read_version(&binary);
    if let Err(code) = version {
        return fail(code, "failed to read skiller version");
    }
    let version = version.unwrap();
    let cmp = compare_versions(&version, &opts.min_version);
    if cmp.is_none() {
        return fail(EXIT_INVALID, &format!("invalid skiller version: {}", version));
    }
    if cmp.unwrap() < 0 {
        return fail(
            EXIT_TOO_OLD,
            &format!("skiller {} is older than required {}", version, opts.min_version),
        );
    }
    EnsureResult {
        ok: true,
        path: binary.to_string_lossy().to_string(),
        version,
        code: 0,
        message: String::new(),
    }
}

fn fail(code: i32, message: &str) -> EnsureResult {
    EnsureResult {
        ok: false,
        path: String::new(),
        version: String::new(),
        code,
        message: message.to_string(),
    }
}

fn find_binary() -> Option<PathBuf> {
    if let Ok(path) = env::var("SKILLER_BIN") {
        let path = PathBuf::from(path);
        if is_executable(&path) {
            return Some(path);
        }
    }
    if let Ok(path_var) = env::var("PATH") {
        for dir in env::split_paths(&path_var) {
            for name in executable_names() {
                let candidate = dir.join(name);
                if is_executable(&candidate) {
                    return Some(candidate);
                }
            }
        }
    }
    if let Some(home) = home_dir() {
        let candidate = home.join(".local").join("bin").join(executable_names()[0]);
        if is_executable(&candidate) {
            return Some(candidate);
        }
    }
    None
}

fn executable_names() -> Vec<&'static str> {
    if cfg!(windows) {
        vec!["skiller.exe", "skiller"]
    } else {
        vec!["skiller"]
    }
}

fn is_executable(path: &Path) -> bool {
    if !path.is_file() {
        return false;
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::metadata(path)
            .map(|m| m.permissions().mode() & 0o111 != 0)
            .unwrap_or(false)
    }
    #[cfg(not(unix))]
    {
        true
    }
}

fn read_version(binary: &Path) -> Result<String, i32> {
    let out = Command::new(binary)
        .args(["version", "--json"])
        .output()
        .map_err(|_| EXIT_EXEC)?;
    if !out.status.success() {
        return Err(EXIT_EXEC);
    }
    let stdout = String::from_utf8(out.stdout).map_err(|_| EXIT_INVALID)?;
    extract_json_string(&stdout, "version").ok_or(EXIT_INVALID)
}

fn extract_json_string(input: &str, key: &str) -> Option<String> {
    let needle = format!("\"{}\"", key);
    let key_pos = input.find(&needle)?;
    let after_key = &input[key_pos + needle.len()..];
    let colon = after_key.find(':')?;
    let after_colon = after_key[colon + 1..].trim_start();
    if !after_colon.starts_with('"') {
        return None;
    }
    let rest = &after_colon[1..];
    let end = rest.find('"')?;
    Some(rest[..end].to_string())
}

fn compare_versions(found: &str, minimum: &str) -> Option<i32> {
    let a = parse_version(found)?;
    let b = parse_version(minimum)?;
    for i in 0..3 {
        if a[i] > b[i] {
            return Some(1);
        }
        if a[i] < b[i] {
            return Some(-1);
        }
    }
    Some(0)
}

fn parse_version(value: &str) -> Option<[u64; 3]> {
    let trimmed = value.trim().trim_start_matches('v');
    let mut out = [0_u64; 3];
    for (i, part) in trimmed.split(|c| c == '.' || c == '-' || c == '+').take(3).enumerate() {
        out[i] = part.parse().ok()?;
    }
    Some(out)
}

fn run_install(command: &str) -> bool {
    let status = if cfg!(windows) {
        Command::new("cmd").args(["/C", command]).status()
    } else {
        Command::new("/bin/sh").args(["-c", command]).status()
    };
    status.map(|s| s.success()).unwrap_or(false)
}

fn home_dir() -> Option<PathBuf> {
    env::var_os("HOME").map(PathBuf::from)
}
