use tiny_loop::tool::tool;

use super::utils::{truncate_single_line, truncate_text_by_lines};

const DEFAULT_NUM_LINES: usize = 250;
const DEFAULT_MAX_LINE_LEN: usize = 200;

fn process_file_content(
    content: String,
    start_line: usize,
    num_lines: usize,
    max_line_len: usize,
) -> String {
    // Truncate content to requested line range
    let result = truncate_text_by_lines(content, start_line, num_lines);
    let truncated_lines = result.truncated;

    // Process each line: truncate if too long and add hint
    let output = result
        .content
        .lines()
        .enumerate()
        .map(|(idx, line)| {
            let line_result = truncate_single_line(line.to_string(), max_line_len);
            if line_result.truncated {
                format!(
                    "{} [Hint: Line {} truncated, use max_line_len to increase limit]",
                    line_result.content,
                    start_line + idx
                )
            } else {
                line_result.content
            }
        })
        .collect::<Vec<_>>()
        .join("\n");

    // Add hint for reading more lines if content was truncated
    if truncated_lines {
        format!(
            "{}\nHint: Use start_line={} to read more.",
            output,
            start_line + num_lines
        )
    } else {
        output
    }
}

/// Read file contents with optional line range
#[tool]
pub async fn read(
    /// File paths
    path: Vec<String>,
    /// Optional start line index (default: 0)
    start_line: Option<usize>,
    /// Optional number of lines to return (default: 250)
    num_lines: Option<usize>,
    /// Optional maximum characters per line (default: 200)
    max_line_len: Option<usize>,
) -> String {
    let start = start_line.unwrap_or(0);
    let num = num_lines.unwrap_or(DEFAULT_NUM_LINES);
    let max_len = max_line_len.unwrap_or(DEFAULT_MAX_LINE_LEN);

    let mut results = Vec::new();
    for p in path {
        let result = match tokio::fs::read_to_string(&p).await {
            Ok(content) => format!(
                "=== {} ===\n{}",
                p,
                process_file_content(content, start, num, max_len)
            ),
            Err(e) => format!("=== {} ===\nError reading file: {}", p, e),
        };
        results.push(result);
    }
    results.join("\n\n")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_process_file_content_no_truncation() {
        let content = "line1\nline2\nline3".to_string();
        let result = process_file_content(content, 0, 100, 200);
        assert_eq!(result, "line1\nline2\nline3");
    }

    #[test]
    fn test_process_file_content_with_line_truncation() {
        let content = "line1\nline2\nline3\nline4".to_string();
        let result = process_file_content(content, 0, 2, 200);
        assert!(result.contains("line1"));
        assert!(result.contains("line2"));
        assert!(result.contains("truncated [2/4 lines]"));
    }

    #[test]
    fn test_process_file_content_with_start_line() {
        let content = "line1\nline2\nline3\nline4".to_string();
        let result = process_file_content(content, 2, 100, 200);
        assert_eq!(result, "line3\nline4");
    }

    #[test]
    fn test_process_file_content_with_single_line_truncation() {
        let content = "short\nthis is a very long line that should be truncated".to_string();
        let result = process_file_content(content, 0, 100, 10);
        assert!(result.contains("short"));
        assert!(result.contains("this is a ... [truncated 10/"));
    }
}
