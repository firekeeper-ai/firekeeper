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
            output.push_str(&format!("## Rule: {}\n\n", rule));
            for violation in violations {
                output.push_str(&format!(
                    "- Lines {}-{}: {}\n",
                    violation.start_line, violation.end_line, violation.detail
                ));
            }
            if let Some(tip) = tips_by_rule.get(rule) {
                let trimmed_tip = tip.trim();
                if !trimmed_tip.is_empty() {
                    output.push_str(&format!("\n**Tip:** {}\n", trimmed_tip));
                }
            }
            output.push('\n');
        }
    }
    output.trim_end().to_string()
}

/// Format trace entries as Markdown with rule details, files, and agent messages
pub fn format_trace_markdown(traces: &[TraceEntry]) -> String {
    let mut output = String::new();
    for trace in traces {
        output.push_str(&format!("# Worker: {}\n\n", trace.worker_id));
        output.push_str(&format!("## Rule: {}\n\n", trace.rule_name));
        let backticks = get_fence_backticks(&trace.rule_instruction);
        output.push_str(&format!(
            "{}markdown\n{}\n{}\n\n",
            backticks,
            trace.rule_instruction.trim(),
            backticks
        ));
        output.push_str(&format!(
            "**Elapsed:** {:.prec$}s\n\n",
            trace.elapsed_secs,
            prec = ELAPSED_TIME_PRECISION
        ));

        output.push_str("## Files\n\n");
        for file in &trace.files {
            output.push_str(&format!("- {}\n", file));
        }

        output.push_str("\n## Tools\n\n");
        let tools_json = serde_json::to_string_pretty(&trace.tools).unwrap_or_default();
        output.push_str(&format!("```json\n{}\n```\n\n", tools_json));

        output.push_str("## Messages\n\n");
        for (i, msg) in trace.messages.iter().enumerate() {
            let (role, content, tool_calls) = match msg {
                Message::System(m) => ("system", Some(m.content.as_str()), None),
                Message::User(m) => ("user", Some(m.content.as_str()), None),
                Message::Assistant(m) => {
                    ("assistant", Some(m.content.as_str()), m.tool_calls.as_ref())
                }
                Message::Tool(m) => ("tool", Some(m.content.as_str()), None),
                Message::Custom(m) => (
                    m.role.as_str(),
                    m.body.get("content").and_then(|v| v.as_str()),
                    None,
                ),
            };

            output.push_str(&format!("### Message {} - Role: {}\n\n", i + 1, role));

            if let Some(content) = content {
                if !content.is_empty() {
                    let backticks = get_fence_backticks(content);
                    if role == "tool" {
                        output.push_str(&format!("{}\n{}\n{}\n\n", backticks, content, backticks));
                    } else if role == "system" || role == "user" {
                        output.push_str(&format!(
                            "{}markdown\n{}\n{}\n\n",
                            backticks, content, backticks
                        ));
                    } else {
                        output.push_str(&format!("{}\n\n", content));
                    }
                }
            }

            if let Some(tool_calls) = tool_calls {
                output.push_str("**Tool Calls:**\n\n");
                for tc in tool_calls {
                    if tc.function.name == crate::tool::think::ThinkArgs::TOOL_NAME {
                        if let Ok(args) = serde_json::from_str::<crate::tool::think::ThinkArgs>(
                            &tc.function.arguments,
                        ) {
                            let backticks = get_fence_backticks(&args._reasoning);
                            output.push_str(&format!(
                                "- **{}**\n\n{}markdown\n{}\n{}\n\n",
                                tc.function.name, backticks, args._reasoning, backticks
                            ));
                        }
                    } else {
                        let formatted_args =
                            serde_json::from_str::<serde_json::Value>(&tc.function.arguments)
                                .and_then(|v| serde_json::to_string_pretty(&v))
                                .unwrap_or_else(|_| tc.function.arguments.clone());
                        output.push_str(&format!(
                            "- **{}**\n\n```json\n{}\n```\n\n",
                            tc.function.name, formatted_args
                        ));
                    }
                }
            }
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
