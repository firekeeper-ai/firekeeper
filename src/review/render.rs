use crate::types::Violation;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use tiny_loop::tool::ToolArgs;
use tiny_loop::types::Message;

const ELAPSED_TIME_PRECISION: usize = 2;

/// Trace entry containing worker task details and agent conversation
#[derive(Serialize, Deserialize)]
pub struct TraceEntry {
    /// Unique identifier for the worker
    pub worker_id: String,
    /// Name of the rule being checked
    pub rule_name: String,
    /// Instruction text for the rule
    pub rule_instruction: String,
    /// List of files being reviewed
    pub files: Vec<String>,
    /// Time taken to complete the task in seconds
    pub elapsed_secs: f64,
    /// Tool definitions available to the agent
    pub tools: Vec<serde_json::Value>,
    /// Conversation messages between agent and tools
    pub messages: Vec<Message>,
}

fn format_violation(violation: &Violation) -> String {
    format!(
        "- Lines {}-{}: {}\n",
        violation.start_line, violation.end_line, violation.detail
    )
}

fn format_tip(tip: &str) -> Option<String> {
    let trimmed = tip.trim();
    if trimmed.is_empty() {
        None
    } else {
        Some(format!("\n**Tip:** {}\n", trimmed))
    }
}

fn format_rule(rule: &str) -> String {
    format!("## Rule: {}\n\n", rule)
}

fn format_rule_violations(rule: &str, violations: &[Violation], tip: Option<&str>) -> String {
    let mut output = format_rule(rule);
    for violation in violations {
        output.push_str(&format_violation(violation));
    }
    if let Some(t) = tip.and_then(|t| format_tip(t)) {
        output.push_str(&t);
    }
    output.push('\n');
    output
}

pub fn format_violations(
    violations_by_file: &HashMap<String, HashMap<String, Vec<Violation>>>,
    tips_by_rule: &HashMap<String, String>,
) -> String {
    if violations_by_file.is_empty() {
        return "No violations found".to_string();
    }

    let mut output = String::new();
    for (file, rules) in violations_by_file {
        output.push_str(&format!("# Violations in {}\n\n", file));
        for (rule, violations) in rules {
            output.push_str(&format_rule_violations(
                rule,
                violations,
                tips_by_rule.get(rule.as_str()).map(|s| s.as_str()),
            ));
        }
    }
    output.trim_end().to_string()
}

fn format_tools(tools: &[serde_json::Value]) -> String {
    let tools_json = serde_json::to_string_pretty(tools).unwrap_or_default();
    format!("## Tools\n\n```json\n{}\n```\n\n", tools_json)
}

fn format_files(files: &[String]) -> String {
    let mut output = String::from("## Files\n\n");
    for file in files {
        output.push_str(&format!("- {}\n", file));
    }
    output.push('\n');
    output
}

fn format_tool_call(tc: &tiny_loop::types::ToolCall) -> String {
    if tc.function.name == crate::tool::think::ThinkArgs::TOOL_NAME {
        if let Ok(args) =
            serde_json::from_str::<crate::tool::think::ThinkArgs>(&tc.function.arguments)
        {
            let backticks = get_fence_backticks(&args._reasoning);
            return format!(
                "- **{}**\n\n{}markdown\n{}\n{}\n\n",
                tc.function.name, backticks, args._reasoning, backticks
            );
        }
    }
    let formatted_args = serde_json::from_str::<serde_json::Value>(&tc.function.arguments)
        .and_then(|v| serde_json::to_string_pretty(&v))
        .unwrap_or_else(|_| tc.function.arguments.clone());
    format!(
        "- **{}**\n\n```json\n{}\n```\n\n",
        tc.function.name, formatted_args
    )
}

fn format_message_content(role: &str, content: &str) -> String {
    let backticks = get_fence_backticks(content);
    match role {
        "tool" => format!("{}\n{}\n{}\n\n", backticks, content, backticks),
        "system" | "user" => format!("{}markdown\n{}\n{}\n\n", backticks, content, backticks),
        _ => format!("{}\n\n", content),
    }
}

fn format_message(msg: &Message, index: usize) -> String {
    let (role, content, tool_calls) = match msg {
        Message::System(m) => ("system", Some(m.content.as_str()), None),
        Message::User(m) => ("user", Some(m.content.as_str()), None),
        Message::Assistant(m) => ("assistant", Some(m.content.as_str()), m.tool_calls.as_ref()),
        Message::Tool(m) => ("tool", Some(m.content.as_str()), None),
        Message::Custom(m) => (
            m.role.as_str(),
            m.body.get("content").and_then(|v| v.as_str()),
            None,
        ),
    };

    let mut output = format!("### Message {} - Role: {}\n\n", index + 1, role);

    if let Some(content) = content {
        if !content.is_empty() {
            output.push_str(&format_message_content(role, content));
        }
    }

    if let Some(tool_calls) = tool_calls {
        output.push_str("**Tool Calls:**\n\n");
        for tc in tool_calls {
            output.push_str(&format_tool_call(tc));
        }
    }

    output
}

fn format_trace_rule(rule_name: &str, rule_instruction: &str) -> String {
    let backticks = get_fence_backticks(rule_instruction);
    format!(
        "## Rule: {}\n\n{}markdown\n{}\n{}\n\n",
        rule_name,
        backticks,
        rule_instruction.trim(),
        backticks
    )
}

/// Format trace entries as Markdown with rule details, files, and agent messages
pub fn format_trace_markdown(traces: &[TraceEntry]) -> String {
    let mut output = String::new();
    for trace in traces {
        output.push_str(&format!("# Worker: {}\n\n", trace.worker_id));
        output.push_str(&format_trace_rule(
            &trace.rule_name,
            &trace.rule_instruction,
        ));
        output.push_str(&format!(
            "**Elapsed:** {:.prec$}s\n\n",
            trace.elapsed_secs,
            prec = ELAPSED_TIME_PRECISION
        ));

        output.push_str(&format_files(&trace.files));
        output.push_str(&format_tools(&trace.tools));

        output.push_str("## Messages\n\n");
        for (i, msg) in trace.messages.iter().enumerate() {
            output.push_str(&format_message(msg, i));
        }
        output.push_str("\n---\n\n");
    }
    output
}

/// Get appropriate number of backticks for Markdown code fence
/// Returns at least 3 backticks, or more if content contains backtick sequences
fn get_fence_backticks(content: &str) -> String {
    const MIN_BACKTICKS: usize = 3;
    let max_backticks = content
        .as_bytes()
        .split(|&b| b != b'`')
        .filter(|s| !s.is_empty())
        .map(|s| s.len())
        .max()
        .unwrap_or(0);
    "`".repeat((max_backticks + 1).max(MIN_BACKTICKS))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_get_fence_backticks() {
        assert_eq!(get_fence_backticks("no backticks"), "```");
        assert_eq!(get_fence_backticks("has ` one"), "```");
        assert_eq!(get_fence_backticks("has ``` three"), "````");
        assert_eq!(get_fence_backticks("has ```` four"), "`````");
    }

    #[test]
    fn test_format_violation() {
        let v = Violation {
            file: "test.rs".to_string(),
            start_line: 10,
            end_line: 15,
            detail: "test issue".to_string(),
        };
        assert_eq!(format_violation(&v), "- Lines 10-15: test issue\n");
    }

    #[test]
    fn test_format_tip() {
        assert_eq!(format_tip("  tip  "), Some("\n**Tip:** tip\n".to_string()));
        assert_eq!(format_tip(""), None);
        assert_eq!(format_tip("   "), None);
    }

    #[test]
    fn test_format_trace_rule() {
        let result = format_trace_rule("TestRule", "  instruction  ");
        assert!(result.contains("## Rule: TestRule"));
        assert!(result.contains("instruction"));
        assert!(result.contains("```markdown"));
    }

    #[test]
    fn test_format_rule() {
        assert_eq!(format_rule("TestRule"), "## Rule: TestRule\n\n");
    }

    #[test]
    fn test_format_rule_violations() {
        let violations = vec![
            Violation {
                file: "test.rs".to_string(),
                start_line: 1,
                end_line: 2,
                detail: "issue1".to_string(),
            },
            Violation {
                file: "test.rs".to_string(),
                start_line: 3,
                end_line: 4,
                detail: "issue2".to_string(),
            },
        ];
        let result = format_rule_violations("TestRule", &violations, Some("fix it"));
        assert!(result.contains("## Rule: TestRule"));
        assert!(result.contains("Lines 1-2: issue1"));
        assert!(result.contains("Lines 3-4: issue2"));
        assert!(result.contains("**Tip:** fix it"));
    }

    #[test]
    fn test_format_violations_empty() {
        let violations = HashMap::new();
        let tips = HashMap::new();
        assert_eq!(format_violations(&violations, &tips), "No violations found");
    }

    #[test]
    fn test_format_files() {
        let files = vec!["file1.rs".to_string(), "file2.rs".to_string()];
        let result = format_files(&files);
        assert!(result.contains("## Files"));
        assert!(result.contains("- file1.rs"));
        assert!(result.contains("- file2.rs"));
    }

    #[test]
    fn test_format_tools() {
        let tools = vec![serde_json::json!({"name": "test_tool"})];
        let result = format_tools(&tools);
        assert!(result.contains("## Tools"));
        assert!(result.contains("```json"));
        assert!(result.contains("test_tool"));
    }

    #[test]
    fn test_format_message_content() {
        assert!(format_message_content("tool", "content").starts_with("```"));
        assert!(format_message_content("system", "content").contains("markdown"));
        assert!(format_message_content("user", "content").contains("markdown"));
        assert_eq!(
            format_message_content("assistant", "content"),
            "content\n\n"
        );
    }
}
