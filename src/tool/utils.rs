/// Result of a truncation operation
#[derive(Debug, Clone, PartialEq)]
pub struct TruncateResult {
    pub content: String,
    pub truncated: bool,
}

/// Default number of characters for truncation
pub const DEFAULT_NUM_CHARS: usize = 5000;

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

/// Truncate text by characters and add pagination hint if truncated
pub fn truncate_with_hint(content: String, start: usize, len: usize) -> String {
    let result = truncate_text_by_chars(content, start, len);
    if result.truncated {
        format!(
            "{}\nHint: Use start_char={} to read more.",
            result.content,
            start + len
        )
    } else {
        result.content
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
    fn test_truncate_text_by_chars_overflow_start() {
        let result = truncate_text_by_chars("hello".to_string(), 100, 10);
        assert_eq!(result.content, "");
        assert!(!result.truncated);
    }
}
