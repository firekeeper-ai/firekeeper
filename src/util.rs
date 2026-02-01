use std::collections::HashMap;
use std::process::Command;
use tracing::debug;

const GIT_EMPTY_TREE: &str = "4b825dc642cb6eb9a060e54bf8d69288fbee4904";

pub fn resolve_base(diff_base: &str) -> String {
    let base = if diff_base.is_empty() {
        debug!("Base is empty, checking for uncommitted changes");
        let has_uncommitted = Command::new("git")
            .args(["diff", "--quiet", "HEAD"])
            .status()
            .map(|s| !s.success())
            .unwrap_or(false);
        let detected = if has_uncommitted { "HEAD" } else { "^" };
        debug!("Auto-detected base: {}", detected);
        detected
    } else {
        diff_base
    };

    if base.starts_with('~') || base.starts_with('^') {
        format!("HEAD{}", base)
    } else {
        base.to_string()
    }
}

pub fn get_changed_files(base: &str) -> Vec<String> {
    let output = if base == "ROOT" {
        Command::new("git")
            .args(["ls-files"])
            .output()
            .expect("Failed to execute git ls-files")
    } else {
        Command::new("git")
            .args(["diff", "--name-only", base])
            .output()
            .expect("Failed to execute git diff")
    };

    String::from_utf8_lossy(&output.stdout)
        .lines()
        .map(|s| s.to_string())
        .collect()
}

pub fn get_diffs(base: &str, files: &[String]) -> HashMap<String, String> {
    let mut diffs = HashMap::new();

    let diff_base = if base == "ROOT" { GIT_EMPTY_TREE } else { base };

    for file in files {
        if let Ok(output) = Command::new("git")
            .args(["diff", diff_base, "--", file])
            .output()
        {
            if output.status.success() {
                let diff = String::from_utf8_lossy(&output.stdout).to_string();
                if !diff.is_empty() {
                    diffs.insert(file.clone(), diff);
                }
            }
        }
    }

    diffs
}
