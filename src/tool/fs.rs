use globset::{Glob, GlobSetBuilder};
use grep::searcher::{Searcher, sinks::UTF8};
use std::path::Path;
use tiny_loop::tool::tool;

const MAX_GLOB_DEPTH: usize = 20;
const MAX_GLOB_MATCHES: usize = 1000;

/// List directory contents with optional recursive depth
#[tool]
pub async fn ls(
    /// List directory contents with optional recursive depth
    path: String,
    /// Optional recursion depth (0 for non-recursive)
    depth: Option<usize>,
) -> String {
    let mut items = Vec::new();

    if let Err(e) = list_dir_recursive(&path, depth.unwrap_or(0), 0, "", &mut items).await {
        return format!("Error listing directory: {}", e);
    }

    items.join("\n")
}

fn list_dir_recursive<'a>(
    path: &'a str,
    max_depth: usize,
    current_depth: usize,
    prefix: &'a str,
    items: &'a mut Vec<String>,
) -> std::pin::Pin<Box<dyn std::future::Future<Output = std::io::Result<()>> + Send + 'a>> {
    Box::pin(async move {
        let mut entries = tokio::fs::read_dir(path).await?;
        let mut entry_list = Vec::new();

        while let Some(entry) = entries.next_entry().await? {
            entry_list.push(entry);
        }
        entry_list.sort_by_key(|e| e.file_name());

        for entry in entry_list {
            let file_type = entry.file_type().await?;
            let name = entry.file_name();
            let name_str = name.to_string_lossy();

            let type_prefix = if file_type.is_dir() { "d" } else { "f" };
            items.push(format!("{}{} {}", prefix, type_prefix, name_str));

            if file_type.is_dir() && current_depth < max_depth {
                let new_path = entry.path();
                if let Some(path_str) = new_path.to_str() {
                    list_dir_recursive(
                        path_str,
                        max_depth,
                        current_depth + 1,
                        &format!("{}  ", prefix),
                        items,
                    )
                    .await?;
                }
            }
        }

        Ok(())
    })
}

/// Search for regex pattern in a file or directory using ripgrep
#[tool]
pub async fn rg(
    /// Search for regex pattern in a file or directory using ripgrep
    path: String,
    /// Regex pattern
    pattern: String,
    /// Optional: case sensitive search (default: false)
    case_sensitive: bool,
    /// Optional: file type filter (e.g., 'rust', 'js', 'py')
    type_filter: Option<String>,
    /// Optional: glob pattern to filter files (e.g., '*.rs', '*.{js,ts}')
    glob_pattern: Option<String>,
) -> String {
    let path = path.to_string();
    let pattern = pattern.to_string();
    let type_filter = type_filter.map(|s| s.to_string());
    let glob_pattern = glob_pattern.map(|s| s.to_string());

    tokio::task::spawn_blocking(move || {
        let mut matcher_builder = grep::regex::RegexMatcherBuilder::new();
        matcher_builder.case_insensitive(!case_sensitive);

        let matcher = match matcher_builder.build(&pattern) {
            Ok(m) => m,
            Err(e) => return format!("Invalid regex pattern: {}", e),
        };

        let mut matches = Vec::new();
        let mut searcher = Searcher::new();
        let path_obj = std::path::Path::new(&path);

        if path_obj.is_dir() {
            let mut walk_builder = ignore::WalkBuilder::new(&path);

            if let Some(ref type_str) = type_filter {
                let mut types_builder = ignore::types::TypesBuilder::new();
                types_builder.add_defaults();
                types_builder.select(type_str);
                match types_builder.build() {
                    Ok(types) => {
                        walk_builder.types(types);
                    }
                    Err(e) => return format!("Invalid type filter '{}': {}", type_str, e),
                }
            }

            let glob_matcher = if let Some(ref glob_str) = glob_pattern {
                match Glob::new(glob_str) {
                    Ok(g) => Some(g.compile_matcher()),
                    Err(e) => return format!("Invalid glob pattern: {}", e),
                }
            } else {
                None
            };

            for result in walk_builder.build() {
                if let Ok(entry) = result {
                    if entry.file_type().map_or(false, |ft| ft.is_file()) {
                        if let Some(ref gm) = glob_matcher {
                            if !gm.is_match(entry.path()) {
                                continue;
                            }
                        }

                        let _ = searcher.search_path(
                            &matcher,
                            entry.path(),
                            UTF8(|lnum, line| {
                                matches.push(format!(
                                    "{}:{}:{}",
                                    entry.path().display(),
                                    lnum,
                                    line.trim_end()
                                ));
                                Ok(true)
                            }),
                        );
                    }
                }
            }
            matches.join("\n")
        } else {
            searcher
                .search_path(
                    &matcher,
                    &path,
                    UTF8(|lnum, line| {
                        matches.push(format!("{}:{}", lnum, line.trim_end()));
                        Ok(true)
                    }),
                )
                .map(|_| matches.join("\n"))
                .unwrap_or_else(|e| format!("Grep error: {}", e))
        }
    })
    .await
    .unwrap_or_else(|e| format!("Task join error: {}", e))
}

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

fn glob_recursive(
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

        if let Some(path_str) = entry_path.to_str() {
            if globset.is_match(path_str) {
                matches.push(path_str.to_string());
            }
        }

        if entry_path.is_dir() {
            glob_recursive(&entry_path, globset, matches, depth + 1)?;
        }
    }

    Ok(())
}
