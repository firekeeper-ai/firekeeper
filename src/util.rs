use std::collections::HashMap;
use std::process::Command;
use tracing::debug;

const GIT_EMPTY_TREE: &str = "4b825dc642cb6eb9a060e54bf8d69288fbee4904";

/// Represents the base reference for git operations
#[derive(Debug)]
pub enum Base {
    /// Review all files in the repository
    Root,
    /// Review changes against a specific commit
    Commit(String),
}

impl Base {
    /// Parse a base string into a Base enum
    ///
    /// - Empty string: auto-detect HEAD or ^ based on uncommitted changes
    /// - "ROOT": all files
    /// - "^" or "~": relative to HEAD
    /// - Otherwise: commit hash or reference
    pub fn parse(diff_base: &str) -> Self {
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

        if base == "ROOT" {
            Self::Root
        } else if base.starts_with('~') || base.starts_with('^') {
            Self::Commit(format!("HEAD{}", base))
        } else {
            Self::Commit(base.to_string())
        }
    }

    /// Get commit reference if available (None for Root)
    fn as_commit_ref(&self) -> Option<&str> {
        match self {
            Self::Root => None,
            Self::Commit(s) => Some(s),
        }
    }

    /// Get the base reference for git diff operations
    fn as_diff_base(&self) -> &str {
        match self {
            Self::Root => GIT_EMPTY_TREE,
            Self::Commit(s) => s,
        }
    }
}

pub fn get_changed_files(base: &Base) -> Vec<String> {
    let output = match base {
        Base::Root => Command::new("git")
            .args(["ls-files"])
            .output()
            .expect("Failed to execute git ls-files"),
        Base::Commit(commit) => Command::new("git")
            .args(["diff", "--name-only", commit])
            .output()
            .expect("Failed to execute git diff"),
    };

    String::from_utf8_lossy(&output.stdout)
        .lines()
        .map(|s| s.to_string())
        .collect()
}

pub fn get_diffs(base: &Base, files: &[String]) -> HashMap<String, String> {
    let mut diffs = HashMap::new();
    let diff_base = base.as_diff_base();

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

pub fn get_commit_messages(base: &Base) -> String {
    let Some(commit) = base.as_commit_ref() else {
        return String::new();
    };

    let output = Command::new("git")
        .args(["log", "--format=%s", &format!("{}..HEAD", commit)])
        .output()
        .expect("Failed to execute git log");

    String::from_utf8_lossy(&output.stdout).trim().to_string()
}
