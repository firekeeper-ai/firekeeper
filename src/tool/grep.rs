use globset::Glob;
use grep::searcher::{Searcher, sinks::UTF8};
use tiny_loop::tool::tool;

/// Search for regex pattern in a file or directory
#[tool]
pub async fn grep(
    /// File or directory path to search
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
