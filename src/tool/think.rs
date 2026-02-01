use tiny_loop::tool::tool;

/// Think through whether something is a violation (keep reasoning brief and focused).
/// MUST be called before reporting any violations to reason about the findings.
#[tool]
pub async fn think(
    /// Brief reasoning (2-4 sentences) about whether the code violates the rule, considering exceptions and context
    #[serde(rename = "reasoning")]
    _reasoning: String,
) -> String {
    "OK".to_string()
}
