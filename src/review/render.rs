use crate::rule::body::RuleBody;
use crate::types::Violation;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use tiny_loop::tool::ToolArgs;
use tiny_loop::types::{Message, TimedMessage, ToolDefinition};

const ELAPSED_TIME_PRECISION: usize = 2;

/// Trace file schema containing all trace entries
#[derive(Serialize, Deserialize)]
pub struct TraceFile {
    pub version: String,
    pub entries: Vec<TraceEntry>,
}

/// Violation file schema containing violations and tips
#[derive(Serialize, Deserialize)]
pub struct ViolationFile {
    pub version: String,
    pub violations: HashMap<String, HashMap<String, Vec<Violation>>>,
    pub tips: HashMap<String, String>,
}

/// Trace entry containing worker task details and agent conversation
#[derive(Serialize, Deserialize, Clone)]
pub struct TraceEntry {
    /// Unique identifier for the worker
    pub worker_id: String,
    /// Rule being checked
    pub rule: RuleBody,
    /// List of files being reviewed
    pub files: Vec<String>,
    /// Time taken to complete the task in seconds
    pub elapsed_secs: f64,
    /// Tool definitions available to the agent
    pub tools: Vec<ToolDefinition>,
    /// Conversation messages between agent and tools
    pub messages: Vec<TimedMessage>,
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

fn format_tools(tools: &[ToolDefinition]) -> String {
    let tools_yaml = serde_yaml_ng::to_string(tools).unwrap_or_default();
    format!(
        "## Tools\n\n<details>\n<summary>Show tools</summary>\n\n```yaml\n{}\n```\n\n</details>\n\n",
        tools_yaml.trim()
    )
}

fn format_focused_files(files: &[String]) -> String {
    let mut output = String::from("## Focused Files\n\n");
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
            return format!(
                "- **{}**\n\n{}\n\n",
                tc.function.name,
                wrap_in_ref_block(&args.reasoning)
            );
        }
    }
    if tc.function.name == crate::tool::sh::ShArgs::TOOL_NAME {
        if let Ok(args) = serde_json::from_str::<crate::tool::sh::ShArgs>(&tc.function.arguments) {
            if args.start_char.is_none() && args.num_chars.is_none() && args.timeout_secs.is_none()
            {
                return format!(
                    "- **{}**\n\n```sh\n{}\n```\n\n",
                    tc.function.name, args.command
                );
            }
        }
    }
    let formatted_args = serde_json::from_str::<serde_json::Value>(&tc.function.arguments)
        .ok()
        .and_then(|v| serde_yaml_ng::to_string(&v).ok())
        .unwrap_or_else(|| tc.function.arguments.clone());
    format!(
        "- **{}**\n\n```yaml\n{}\n```\n\n",
        tc.function.name,
        formatted_args.trim()
    )
}

fn format_message_content(role: &str, content: &str) -> String {
    match role {
        "tool" => {
            let backticks = get_fence_backticks(content);
            format!("{}\n{}\n{}\n\n", backticks, content, backticks)
        }
        _ => format!("{}\n\n", wrap_in_ref_block(content)),
    }
}

fn format_message_header(
    index: usize,
    role: &str,
    timestamp_str: &str,
    elapsed_secs: f64,
) -> String {
    format!(
        "### {}. {} @ {} (+{:.2}s)\n\n",
        index + 1,
        role,
        timestamp_str,
        elapsed_secs
    )
}

fn format_message(msg: &TimedMessage, index: usize) -> String {
    let (role, content, tool_calls) = match &msg.message {
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

    let timestamp_str = DateTime::<Utc>::from(msg.timestamp)
        .format("%Y-%m-%d %H:%M:%S")
        .to_string();

    let mut output = String::new();

    if let Some(content) = content {
        output.push_str(&format_message_header(
            index,
            role,
            &timestamp_str,
            msg.elapsed.as_secs_f64(),
        ));

        if !content.is_empty() {
            if role == "system" || role == "user" || role == "tool" {
                output.push_str("<details>\n<summary>Show content</summary>\n\n");
                output.push_str(&format_message_content(role, content));
                output.push_str("</details>\n\n");
            } else {
                output.push_str(&format_message_content(role, content));
            }
        } else if tool_calls.is_none() {
            output.push_str("Empty message.\n\n");
        }
    }

    if let Some(tool_calls) = tool_calls {
        if output.is_empty() {
            output.push_str(&format_message_header(
                index,
                role,
                &timestamp_str,
                msg.elapsed.as_secs_f64(),
            ));
        }
        output.push_str("#### Tool Calls\n\n");
        for tc in tool_calls {
            output.push_str(&format_tool_call(tc));
        }
    }

    output
}

fn wrap_in_ref_block(content: &str) -> String {
    content
        .trim()
        .lines()
        .map(|line| format!("> {}", line))
        .collect::<Vec<_>>()
        .join("\n")
}

fn format_trace_rule(rule_name: &str, rule_instruction: &str) -> String {
    format!(
        "## Rule: {}\n\n<details>\n<summary>Show rule</summary>\n\n{}\n\n</details>\n\n",
        rule_name,
        wrap_in_ref_block(rule_instruction)
    )
}

/// Format trace entries as Markdown with rule details, files, and agent messages
pub fn format_trace_markdown(traces: &[TraceEntry]) -> String {
    let mut output = String::new();
    for trace in traces {
        output.push_str(&format!(
            "# Worker: {} (Elapsed: {:.prec$}s)\n\n",
            trace.worker_id,
            trace.elapsed_secs,
            prec = ELAPSED_TIME_PRECISION
        ));
        output.push_str(&format_trace_rule(
            &trace.rule.name,
            &trace.rule.instruction,
        ));

        output.push_str(&format_focused_files(&trace.files));
        output.push_str(&format_tools(&trace.tools));

        output.push_str("## Messages\n\n");
        for (i, msg) in trace.messages.iter().enumerate() {
            output.push_str(&format_message(msg, i));
        }
        output.push_str("---\n\n");
    }
    output
}

/// Get appropriate number of backticks for Markdown code fence
/// Returns at least 3 backticks, or more if content contains backtick sequences
pub(crate) fn get_fence_backticks(content: &str) -> String {
    const MIN_BACKTICKS: usize = 3;
    let max_backticks = content
        .lines()
        .filter(|line| line.chars().all(|c| c == '`'))
        .map(|line| line.len())
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
        assert_eq!(get_fence_backticks("```"), "````");
        assert_eq!(get_fence_backticks("````"), "`````");
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
        assert!(result.contains("> instruction"));
    }

    #[test]
    fn test_wrap_in_ref_block() {
        assert_eq!(wrap_in_ref_block("line1"), "> line1");
        assert_eq!(wrap_in_ref_block("line1\nline2"), "> line1\n> line2");
        assert_eq!(wrap_in_ref_block("  line1  "), "> line1");
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
    fn test_format_focused_files() {
        let files = vec!["file1.rs".to_string(), "file2.rs".to_string()];
        let result = format_focused_files(&files);
        assert!(result.contains("## Focused Files"));
        assert!(result.contains("- file1.rs"));
        assert!(result.contains("- file2.rs"));
    }

    #[test]
    fn test_format_tools() {
        use tiny_loop::types::{Parameters, ToolFunction};

        let tools = vec![ToolDefinition {
            tool_type: "function".to_string(),
            function: ToolFunction {
                name: "test_tool".to_string(),
                description: String::new(),
                parameters: Parameters::from_schema(
                    serde_json::from_value(serde_json::json!({})).unwrap(),
                ),
            },
        }];
        let result = format_tools(&tools);
        assert!(result.contains("## Tools"));
        assert!(result.contains("```yaml"));
        assert!(result.contains("test_tool"));
    }

    #[test]
    fn test_format_message_content() {
        assert!(format_message_content("tool", "content").starts_with("```"));
        assert!(format_message_content("system", "content").starts_with("> "));
        assert!(format_message_content("user", "content").starts_with("> "));
        assert_eq!(
            format_message_content("assistant", "content"),
            "> content\n\n"
        );
    }
}
