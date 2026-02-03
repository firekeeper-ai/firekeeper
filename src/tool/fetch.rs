use tiny_loop::tool::tool;

use super::utils::truncate_text_by_chars;

/// Default number of characters to fetch from a URL
const DEFAULT_NUM_CHARS: usize = 5000;

fn process_html(html: String, start_char: usize, num_chars: usize) -> String {
    let markdown = html2md::parse_html(&html);
    let result = truncate_text_by_chars(markdown, start_char, num_chars);

    if result.truncated {
        format!(
            "{}\nHint: Use start_char={} to read more.",
            result.content,
            start_char + num_chars
        )
    } else {
        result.content
    }
}

/// Fetch webpages and convert HTML to Markdown
#[tool]
pub async fn fetch(
    /// URLs to fetch
    url: Vec<String>,
    /// Optional start character index (default: 0)
    start_char: Option<usize>,
    /// Optional number of characters to return (default: 5000)
    num_chars: Option<usize>,
) -> String {
    if url.len() == 1 {
        return fetch_one(&url[0], start_char, num_chars).await;
    }

    let mut results = Vec::with_capacity(url.len());
    for u in url {
        let content = fetch_one(&u, start_char, num_chars).await;
        results.push(format!("=== {} ===\n{}", u, content));
    }
    results.join("\n\n")
}

async fn fetch_one(url: &str, start_char: Option<usize>, num_chars: Option<usize>) -> String {
    let response = match reqwest::get(url).await {
        Ok(r) => r,
        Err(e) => return format!("Error fetching URL: {}", e),
    };

    let html = match response.text().await {
        Ok(h) => h,
        Err(e) => return format!("Error reading response: {}", e),
    };

    process_html(
        html,
        start_char.unwrap_or(0),
        num_chars.unwrap_or(DEFAULT_NUM_CHARS),
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_process_html_basic() {
        let html = "<h1>Title</h1><p>Content</p>".to_string();
        let result = process_html(html, 0, 5000);
        assert!(result.contains("Title"));
        assert!(result.contains("Content"));
    }

    #[test]
    fn test_process_html_with_truncation() {
        let html = "<p>Hello World</p>".to_string();
        let result = process_html(html, 0, 5);
        assert!(result.contains("truncated"));
    }
}
