/// Result of a truncation operation
#[derive(Debug, Clone, PartialEq)]
pub struct TruncateResult {
    pub content: String,
    pub truncated: bool,
}

/// Truncate text content by characters with pagination support
pub fn truncate_text_by_chars(content: String, start: usize, len: usize) -> TruncateResult {
    let total_len = content.len();
    let start = start.min(total_len);
    let end_idx = start.saturating_add(len).min(total_len);

    let mut result: String = content
        .chars()
        .skip(start)
        .take(end_idx.saturating_sub(start))
        .collect();

    let truncated = end_idx < total_len;
    if truncated {
        result.push_str(&format!(
            "\n\n---\ntruncated [{}/{} chars]",
            end_idx, total_len
        ));
    }

    TruncateResult {
        content: result,
        truncated,
    }
}

/// Truncate text content by lines with pagination support
pub fn truncate_text_by_lines(content: String, start: usize, len: usize) -> TruncateResult {
    let lines: Vec<&str> = content.lines().collect();
    let total_lines = lines.len();
    let start = start.min(total_lines);
    let end_idx = start.saturating_add(len).min(total_lines);

    let mut result = lines[start..end_idx].join("\n");

    let truncated = end_idx < total_lines;
    if truncated {
        result.push_str(&format!(
            "\n\n---\ntruncated [{}/{} lines]",
            end_idx, total_lines
        ));
    }

    TruncateResult {
        content: result,
        truncated,
    }
}

/// Truncate a single line to a maximum length
pub fn truncate_single_line(content: String, max_len: usize) -> TruncateResult {
    let total_len = content.len();
    let truncated = total_len > max_len;

    let result = if truncated {
        format!(
            "{}... [truncated {}/{} chars]",
            &content[..max_len],
            max_len,
            total_len
        )
    } else {
        content
    };

    TruncateResult {
        content: result,
        truncated,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_truncate_text_by_chars_no_truncation() {
        let result = truncate_text_by_chars("hello".to_string(), 0, 5000);
        assert_eq!(result.content, "hello");
        assert!(!result.truncated);
    }

    #[test]
    fn test_truncate_text_by_chars_with_truncation() {
        let content = "a".repeat(6000);
        let result = truncate_text_by_chars(content, 0, 5000);
        assert_eq!(
            result.content,
            format!("{}\n\n---\ntruncated [5000/6000 chars]", "a".repeat(5000))
        );
        assert!(result.truncated);
    }

    #[test]
    fn test_truncate_text_by_chars_with_start() {
        let result = truncate_text_by_chars("0123456789".to_string(), 5, 5000);
        assert_eq!(result.content, "56789");
        assert!(!result.truncated);
    }

    #[test]
    fn test_truncate_text_by_lines_no_truncation() {
        let result = truncate_text_by_lines("line1\nline2\nline3".to_string(), 0, 5);
        assert_eq!(result.content, "line1\nline2\nline3");
        assert!(!result.truncated);
    }

    #[test]
    fn test_truncate_text_by_lines_with_truncation() {
        let result = truncate_text_by_lines("line1\nline2\nline3\nline4".to_string(), 0, 2);
        assert_eq!(result.content, "line1\nline2\n\n---\ntruncated [2/4 lines]");
        assert!(result.truncated);
    }

    #[test]
    fn test_truncate_text_by_lines_with_start() {
        let result = truncate_text_by_lines("line1\nline2\nline3\nline4".to_string(), 2, 10);
        assert_eq!(result.content, "line3\nline4");
        assert!(!result.truncated);
    }

    #[test]
    fn test_truncate_text_by_lines_overflow_start() {
        let result = truncate_text_by_lines("line1\nline2\nline3".to_string(), 80, 10);
        assert_eq!(result.content, "");
        assert!(!result.truncated);
    }

    #[test]
    fn test_truncate_text_by_chars_overflow_start() {
        let result = truncate_text_by_chars("hello".to_string(), 100, 10);
        assert_eq!(result.content, "");
        assert!(!result.truncated);
    }

    #[test]
    fn test_truncate_single_line_no_truncation() {
        let result = truncate_single_line("hello".to_string(), 10);
        assert_eq!(result.content, "hello");
        assert!(!result.truncated);
    }

    #[test]
    fn test_truncate_single_line_with_truncation() {
        let result = truncate_single_line("hello world".to_string(), 5);
        assert_eq!(result.content, "hello... [truncated 5/11 chars]");
        assert!(result.truncated);
    }
}
