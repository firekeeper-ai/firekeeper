use serde_json::json;
use std::path::Path;
use grep::regex::RegexMatcher;
use grep::searcher::{Searcher, sinks::UTF8};
use globset::{Glob, GlobSetBuilder};
use tracing::debug;

use crate::agent::types::{Tool, ToolFunction};

pub fn create_fs_tools() -> Vec<Tool> {
    vec![
        Tool {
            tool_type: "function".to_string(),
            function: ToolFunction {
                name: "read".to_string(),
                description: "Read file contents with optional line range".to_string(),
                parameters: json!({
                    "type": "object",
                    "properties": {
                        "path": {"type": "string", "description": "File path"},
                        "start_line": {"type": "integer", "description": "Optional start line (1-indexed)"},
                        "end_line": {"type": "integer", "description": "Optional end line (inclusive)"},
                        "show_line_numbers": {"type": "boolean", "description": "Optional: show line numbers (default: false)"},
                        "limit": {"type": "integer", "description": "Optional: maximum number of lines to return (default: 1000)"}
                    },
                    "required": ["path"]
                }),
            },
        },
        Tool {
            tool_type: "function".to_string(),
            function: ToolFunction {
                name: "ls".to_string(),
                description: "List directory contents with optional recursive depth".to_string(),
                parameters: json!({
                    "type": "object",
                    "properties": {
                        "path": {"type": "string", "description": "Directory path"},
                        "depth": {"type": "integer", "description": "Optional recursion depth (0 for non-recursive)"}
                    },
                    "required": ["path"]
                }),
            },
        },
        Tool {
            tool_type: "function".to_string(),
            function: ToolFunction {
                name: "rg".to_string(),
                description: "Search for regex pattern in a file using ripgrep".to_string(),
                parameters: json!({
                    "type": "object",
                    "properties": {
                        "path": {"type": "string", "description": "File path"},
                        "pattern": {"type": "string", "description": "Regex pattern"}
                    },
                    "required": ["path", "pattern"]
                }),
            },
        },
        Tool {
            tool_type: "function".to_string(),
            function: ToolFunction {
                name: "glob".to_string(),
                description: "Find files matching a glob pattern".to_string(),
                parameters: json!({
                    "type": "object",
                    "properties": {
                        "path": {"type": "string", "description": "Directory path to search"},
                        "pattern": {"type": "string", "description": "Glob pattern (e.g., **/*.rs)"}
                    },
                    "required": ["path", "pattern"]
                }),
            },
        },
    ]
}

/// Read file contents with optional line range.
/// Lines are 1-indexed and prefixed with line numbers if show_line_numbers is true.
pub async fn read_file(path: &str, start_line: Option<usize>, end_line: Option<usize>, show_line_numbers: bool, limit: usize) -> Result<String, String> {
    debug!("Reading file: {}", path);
    match tokio::fs::read_to_string(path).await {
        Ok(content) => {
            let lines: Vec<&str> = content.lines().collect();
            
            let start = start_line.unwrap_or(1).saturating_sub(1);
            let requested_end = end_line.unwrap_or(lines.len()).min(lines.len());
            let end = requested_end.min(start + limit);
            
            if start >= lines.len() {
                return Err(format!("Start line {} is beyond file length {}", start + 1, lines.len()));
            }
            
            let selected_lines: Vec<String> = lines[start..end]
                .iter()
                .enumerate()
                .map(|(i, line)| {
                    if show_line_numbers {
                        format!("{}: {}", start + i + 1, line)
                    } else {
                        line.to_string()
                    }
                })
                .collect();
            
            let mut result = selected_lines.join("\n");
            
            if end < requested_end {
                result.push_str(&format!("\n\n[Output truncated: showing {} of {} lines]", end - start, requested_end - start));
            }
            
            Ok(result)
        }
        Err(e) => Err(format!("Error reading file: {}", e)),
    }
}

/// List directory contents recursively up to specified depth.
/// Entries are prefixed with 'd' for directories and 'f' for files.
pub async fn list_dir(path: &str, depth: Option<usize>) -> Result<String, String> {
    debug!("Listing directory: {} (depth: {:?})", path, depth);
    let mut items = Vec::new();
    
    if let Err(e) = list_dir_recursive(path, depth.unwrap_or(0), 0, "", &mut items).await {
        return Err(format!("Error listing directory: {}", e));
    }
    
    Ok(items.join("\n"))
}

fn list_dir_recursive<'a>(
    path: &'a str,
    max_depth: usize,
    current_depth: usize,
    prefix: &'a str,
    items: &'a mut Vec<String>,
) -> std::pin::Pin<Box<dyn std::future::Future<Output = std::io::Result<()>> + 'a>> {
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
                    list_dir_recursive(path_str, max_depth, current_depth + 1, &format!("{}  ", prefix), items).await?;
                }
            }
        }
        
        Ok(())
    })
}

/// Grep a path using regex pattern with ripgrep.
/// Returns matches in format "line_number:line_content".
pub async fn grep(path: &str, pattern: &str) -> Result<String, String> {
    debug!("Grepping path: {} for pattern: {}", path, pattern);
    let path = path.to_string();
    let pattern = pattern.to_string();
    
    tokio::task::spawn_blocking(move || {
        let matcher = match RegexMatcher::new(&pattern) {
            Ok(m) => m,
            Err(e) => return Err(format!("Invalid regex pattern: {}", e)),
        };
        
        let mut matches = Vec::new();
        let mut searcher = Searcher::new();
        
        let result = searcher.search_path(
            &matcher,
            &path,
            UTF8(|lnum, line| {
                matches.push(format!("{}:{}", lnum, line.trim_end()));
                Ok(true)
            }),
        );
        
        match result {
            Ok(_) => Ok(matches.join("\n")),
            Err(e) => Err(format!("Grep error: {}", e)),
        }
    }).await.unwrap_or_else(|e| Err(format!("Task join error: {}", e)))
}

/// Find files matching a glob pattern recursively.
/// Searches up to depth 20 and returns up to 1000 matches.
pub async fn glob_files(path: &str, pattern: &str) -> Result<String, String> {
    debug!("Globbing files in: {} with pattern: {}", path, pattern);
    let path = path.to_string();
    let pattern = pattern.to_string();
    
    tokio::task::spawn_blocking(move || {
        let glob = match Glob::new(&pattern) {
            Ok(g) => g,
            Err(e) => return Err(format!("Invalid glob pattern: {}", e)),
        };
        
        let mut builder = GlobSetBuilder::new();
        builder.add(glob);
        let globset = match builder.build() {
            Ok(gs) => gs,
            Err(e) => return Err(format!("Failed to build globset: {}", e)),
        };
        
        let mut matches = Vec::new();
        if let Err(e) = glob_recursive(Path::new(&path), &globset, &mut matches, 0) {
            return Err(format!("Error searching: {}", e));
        }
        
        Ok(matches.join("\n"))
    }).await.unwrap_or_else(|e| Err(format!("Task join error: {}", e)))
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
