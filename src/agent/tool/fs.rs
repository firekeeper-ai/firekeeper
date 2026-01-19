use serde::Serialize;
use std::fs;
use std::path::Path;
use grep::regex::RegexMatcher;
use grep::searcher::{Searcher, sinks::UTF8};
use globset::{Glob, GlobSetBuilder};
use tracing::debug;

/// Result of a file system operation.
#[derive(Debug, Serialize)]
pub struct FsResult {
    pub success: bool,
    pub content: String,
}

/// Search match with line number and context.
#[derive(Debug, Serialize)]
struct SearchMatch {
    line_number: usize,
    context: String,
}

/// Read file contents with optional line range.
/// Lines are 1-indexed and prefixed with line numbers.
pub fn read_file(path: &str, start_line: Option<usize>, end_line: Option<usize>) -> FsResult {
    debug!("Reading file: {}", path);
    match fs::read_to_string(path) {
        Ok(content) => {
            let lines: Vec<&str> = content.lines().collect();
            
            let start = start_line.unwrap_or(1).saturating_sub(1);
            let end = end_line.unwrap_or(lines.len()).min(lines.len());
            
            if start >= lines.len() {
                return FsResult {
                    success: false,
                    content: format!("Start line {} is beyond file length {}", start + 1, lines.len()),
                };
            }
            
            let selected_lines: Vec<String> = lines[start..end]
                .iter()
                .enumerate()
                .map(|(i, line)| format!("{}: {}", start + i + 1, line))
                .collect();
            
            FsResult {
                success: true,
                content: selected_lines.join("\n"),
            }
        }
        Err(e) => FsResult {
            success: false,
            content: format!("Error reading file: {}", e),
        },
    }
}

/// List directory contents recursively up to specified depth.
/// Entries are prefixed with 'd' for directories and 'f' for files.
pub fn list_dir(path: &str, depth: Option<usize>) -> FsResult {
    debug!("Listing directory: {} (depth: {:?})", path, depth);
    let mut items = Vec::new();
    
    if let Err(e) = list_dir_recursive(path, depth.unwrap_or(0), 0, "", &mut items) {
        return FsResult {
            success: false,
            content: format!("Error listing directory: {}", e),
        };
    }
    
    FsResult {
        success: true,
        content: items.join("\n"),
    }
}

fn list_dir_recursive(
    path: &str,
    max_depth: usize,
    current_depth: usize,
    prefix: &str,
    items: &mut Vec<String>,
) -> std::io::Result<()> {
    let entries = fs::read_dir(path)?;
    let mut entries: Vec<_> = entries.flatten().collect();
    entries.sort_by_key(|e| e.file_name());
    
    for entry in entries {
        let file_type = entry.file_type()?;
        let name = entry.file_name();
        let name_str = name.to_string_lossy();
        
        let type_prefix = if file_type.is_dir() { "d" } else { "f" };
        items.push(format!("{}{} {}", prefix, type_prefix, name_str));
        
        if file_type.is_dir() && current_depth < max_depth {
            let new_path = entry.path();
            if let Some(path_str) = new_path.to_str() {
                list_dir_recursive(path_str, max_depth, current_depth + 1, &format!("{}  ", prefix), items)?;
            }
        }
    }
    
    Ok(())
}

/// Search for a pattern in a file with context lines (case-insensitive).
/// Returns JSON array of SearchMatch objects.
pub fn search_in_file(path: &str, pattern: &str, context_lines: usize) -> FsResult {
    debug!("Searching in file: {} for pattern: {}", path, pattern);
    let content = match fs::read_to_string(path) {
        Ok(c) => c,
        Err(e) => return FsResult {
            success: false,
            content: format!("Error reading file: {}", e),
        },
    };
    
    let lines: Vec<&str> = content.lines().collect();
    let pattern_lower = pattern.to_lowercase();
    let mut matches = Vec::new();
    
    for (line_num, line) in lines.iter().enumerate() {
        if line.to_lowercase().contains(&pattern_lower) {
            let start = line_num.saturating_sub(context_lines);
            let end = lines.len().min(line_num + context_lines + 1);
            
            let mut context_text = Vec::new();
            for i in start..end {
                let prefix = if i == line_num { "→ " } else { "  " };
                context_text.push(format!("{}{}: {}", prefix, i + 1, lines[i]));
            }
            
            matches.push(SearchMatch {
                line_number: line_num + 1,
                context: context_text.join("\n"),
            });
        }
    }
    
    match serde_json::to_string(&matches) {
        Ok(json) => FsResult {
            success: true,
            content: json,
        },
        Err(e) => FsResult {
            success: false,
            content: format!("Error serializing results: {}", e),
        },
    }
}

/// Grep a path using regex pattern with ripgrep.
/// Returns matches in format "line_number:line_content".
pub fn grep(path: &str, pattern: &str) -> FsResult {
    debug!("Grepping path: {} for pattern: {}", path, pattern);
    let matcher = match RegexMatcher::new(pattern) {
        Ok(m) => m,
        Err(e) => return FsResult {
            success: false,
            content: format!("Invalid regex pattern: {}", e),
        },
    };
    
    let mut matches = Vec::new();
    let mut searcher = Searcher::new();
    
    let result = searcher.search_path(
        &matcher,
        path,
        UTF8(|lnum, line| {
            matches.push(format!("{}:{}", lnum, line.trim_end()));
            Ok(true)
        }),
    );
    
    match result {
        Ok(_) => FsResult {
            success: true,
            content: matches.join("\n"),
        },
        Err(e) => FsResult {
            success: false,
            content: format!("Grep error: {}", e),
        },
    }
}

/// Find files matching a glob pattern recursively.
/// Searches up to depth 20 and returns up to 1000 matches.
pub fn glob_files(path: &str, pattern: &str) -> FsResult {
    debug!("Globbing files in: {} with pattern: {}", path, pattern);
    let glob = match Glob::new(pattern) {
        Ok(g) => g,
        Err(e) => return FsResult {
            success: false,
            content: format!("Invalid glob pattern: {}", e),
        },
    };
    
    let mut builder = GlobSetBuilder::new();
    builder.add(glob);
    let globset = match builder.build() {
        Ok(gs) => gs,
        Err(e) => return FsResult {
            success: false,
            content: format!("Failed to build globset: {}", e),
        },
    };
    
    let mut matches = Vec::new();
    if let Err(e) = glob_recursive(Path::new(path), &globset, &mut matches, 0) {
        return FsResult {
            success: false,
            content: format!("Error searching: {}", e),
        };
    }
    
    FsResult {
        success: true,
        content: matches.join("\n"),
    }
}

fn glob_recursive(
    path: &Path,
    globset: &globset::GlobSet,
    matches: &mut Vec<String>,
    depth: usize,
) -> std::io::Result<()> {
    if depth > 20 || matches.len() >= 1000 {
        return Ok(());
    }
    
    for entry in fs::read_dir(path)? {
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
