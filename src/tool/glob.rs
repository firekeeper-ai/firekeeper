use globset::{Glob, GlobSetBuilder};
use std::path::Path;
use tiny_loop::tool::tool;

const MAX_GLOB_DEPTH: usize = 20;
const MAX_GLOB_MATCHES: usize = 1000;

/// Find files matching a glob pattern
#[tool]
pub async fn glob(
    /// Directory path to search
    path: String,
    /// Glob pattern (e.g., **/*.rs)
    pattern: String,
) -> String {
    let path = path.to_string();
    let pattern = pattern.to_string();

    tokio::task::spawn_blocking(move || {
        let glob = match Glob::new(&pattern) {
            Ok(g) => g,
            Err(e) => return format!("Invalid glob pattern: {}", e),
        };

        let mut builder = GlobSetBuilder::new();
        builder.add(glob);
        let globset = match builder.build() {
            Ok(gs) => gs,
            Err(e) => return format!("Failed to build globset: {}", e),
        };

        let mut matches = Vec::new();
        if let Err(e) = glob_recursive(Path::new(&path), &globset, &mut matches, 0) {
            return format!("Error searching: {}", e);
        }

        matches.join("\n")
    })
    .await
    .unwrap_or_else(|e| format!("Task join error: {}", e))
}

pub fn glob_recursive(
    path: &Path,
    globset: &globset::GlobSet,
    matches: &mut Vec<String>,
    depth: usize,
) -> std::io::Result<()> {
    if depth > MAX_GLOB_DEPTH || matches.len() >= MAX_GLOB_MATCHES {
        return Ok(());
    }

    for entry in std::fs::read_dir(path)? {
        let entry = entry?;
        let entry_path = entry.path();

        if entry_path.is_file() {
            if let Some(path_str) = entry_path.to_str() {
                let relative = path_str.strip_prefix("./").unwrap_or(path_str);
                if globset.is_match(path_str) || globset.is_match(relative) {
                    matches.push(path_str.to_string());
                }
            }
        }

        if entry_path.is_dir() {
            glob_recursive(&entry_path, globset, matches, depth + 1)?;
        }
    }

    Ok(())
}
