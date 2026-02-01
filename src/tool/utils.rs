/// Truncate text content with pagination support
pub fn truncate_text(content: String, start: usize, len: usize) -> String {
    let end_idx = start.saturating_add(len).min(content.len());
    let total_len = content.len();

    let mut result: String = content
        .chars()
        .skip(start)
        .take(end_idx.saturating_sub(start))
        .collect();

    if end_idx < total_len {
        result.push_str(&format!(
            "\n\n---\ntruncated [{}/{} chars]",
            end_idx, total_len
        ));
    }

    result
}
