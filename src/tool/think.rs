use tiny_loop::tool::tool;

/// Maximum lines before overthinking warning (brief reasoning should be 2-4 sentences)
const MAX_REASONING_LINES: usize = 10;
/// Maximum characters before overthinking warning (roughly 2-3 paragraphs)
const MAX_REASONING_CHARS: usize = 1500;

/// Think through whether something is a violation (keep reasoning brief and focused).
/// MUST be called before reporting any violations to reason about the findings.
#[tool]
pub async fn think(
    /// Brief reasoning (2-4 sentences) about whether the code violates the rule, considering exceptions and context
    reasoning: String,
) -> String {
    let line_count = reasoning.lines().count();
    let char_count = reasoning.chars().count();

    if line_count > MAX_REASONING_LINES || char_count > MAX_REASONING_CHARS {
        format!(
            "OK. Note: Overthinking detected ({} lines, {} chars). Keep reasoning concise and focused.",
            line_count, char_count
        )
    } else {
        "OK".to_string()
    }
}
