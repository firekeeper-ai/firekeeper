use crate::agent::llm::openai::OpenAIProvider;
use crate::agent::r#loop::AgentLoop;
use crate::agent::tool::{fs, report, web};
use crate::agent::types::ToolCall;
use crate::rule::body::RuleBody;
use std::collections::HashMap;
use tracing::{debug, info, trace};

const DEFAULT_READ_LIMIT: usize = 1000;

/// Worker state containing violations and diffs
pub struct WorkerState {
    pub violations: Vec<report::Violation>,
    pub diffs: HashMap<String, String>,
}

/// Worker result containing violations and optional trace messages
pub struct WorkerResult {
    pub rule_name: String,
    pub rule_instruction: String,
    pub files: Vec<String>,
    pub blocking: bool,
    pub violations: Vec<report::Violation>,
    pub messages: Option<Vec<crate::agent::types::Message>>,
}

/// Run a review worker for a specific rule and set of files
///
/// Returns a WorkerResult containing violations found and optionally the agent conversation trace
pub async fn worker(
    rule: &RuleBody,
    files: Vec<String>,
    base_url: &str,
    api_key: &str,
    model: &str,
    diffs: HashMap<String, String>,
    trace_enabled: bool,
) -> Result<WorkerResult, Box<dyn std::error::Error>> {
    info!(
        "Worker: reviewing {} files for rule '{}'",
        files.len(),
        rule.name
    );
    trace!("Files to review: {:?}", files);

    // Setup LLM provider
    debug!("Creating OpenAI provider with model: {}", model);
    let provider =
        OpenAIProvider::new(base_url.to_string(), api_key.to_string(), model.to_string());
    
    // Setup tools
    let mut tools = fs::create_fs_tools();
    tools.extend(web::create_web_tools());
    tools.extend(report::create_report_tools());
    trace!("Created {} tools", tools.len());
    
    // Setup state and agent
    let state = WorkerState {
        violations: Vec::new(),
        diffs,
    };
    let mut agent = AgentLoop::new(provider, ToolExecutor, tools, state);

    // System message
    trace!("Adding system message to agent");
    agent.add_message(
        "system",
        "You are a code reviewer. Your task is to review code changes against a specific rule. \
        Focus only on the files provided and only check for violations of the given rule. \
        You can read related files if needed, but only report issues related to the provided files and rule. \
        \n\nWorkflow:\n\
        1. Get diffs for the provided files to see what changed\n\
        2. Search/read related files if needed for context\n\
        3. Use the 'think' tool to reason about whether the changes violate the rule\n\
        4. Use the 'report' tool to report all violations found, then exit without summary"
    );

    // User message with rule and files
    let files_list = files.join("\n- ");
    let user_message = format!(
        "Review the following files:\n\n\
        - {}\n\n\
        Against this rule:\n\n\
        <rule>\n{}\n</rule>",
        files_list, rule.instruction
    );
    trace!("Adding user message with {} files", files.len());
    trace!("User message: {}", user_message);
    agent.add_message("user", &user_message);

    // Run agent loop
    debug!("Starting agent loop for rule '{}'", rule.name);
    agent.run().await?;

    let messages = if trace_enabled {
        Some(agent.messages.clone())
    } else {
        None
    };

    Ok(WorkerResult {
        rule_name: rule.name.clone(),
        rule_instruction: rule.instruction.clone(),
        files,
        blocking: rule.blocking,
        violations: agent.state.violations,
        messages,
    })
}

/// Tool executor for worker
struct ToolExecutor;

impl crate::agent::r#loop::ToolExecutor<WorkerState> for ToolExecutor {
    async fn execute(&mut self, tool_call: &ToolCall, state: &mut WorkerState) -> String {
        let args: serde_json::Value = match serde_json::from_str(&tool_call.function.arguments) {
            Ok(v) => v,
            Err(e) => return format!("Error parsing arguments: {}", e),
        };

        let result = match tool_call.function.name.as_str() {
            "read" => {
                fs::read_file(
                    args["path"].as_str().unwrap_or(""),
                    args["start_line"].as_u64().map(|v| v as usize),
                    args["end_line"].as_u64().map(|v| v as usize),
                    args["show_line_numbers"].as_bool().unwrap_or(false),
                    args["limit"].as_u64().map(|v| v as usize).unwrap_or(DEFAULT_READ_LIMIT),
                )
                .await
            }
            "diff" => fs::diff_file(args["path"].as_str().unwrap_or(""), &state.diffs).await,
            "ls" => {
                fs::list_dir(
                    args["path"].as_str().unwrap_or(""),
                    args["depth"].as_u64().map(|v| v as usize),
                )
                .await
            }
            "rg" => {
                fs::grep(
                    args["path"].as_str().unwrap_or(""),
                    args["pattern"].as_str().unwrap_or(""),
                )
                .await
            }
            "glob" => {
                fs::glob_files(
                    args["path"].as_str().unwrap_or(""),
                    args["pattern"].as_str().unwrap_or(""),
                )
                .await
            }
            "fetch" => web::fetch(args["url"].as_str().unwrap_or("")).await,
            "think" => {
                let reasoning = args["reasoning"].as_str().unwrap_or("");
                report::think(reasoning.to_string()).await
            }
            "report" => {
                let violations: Vec<report::Violation> =
                    match serde_json::from_value(args["violations"].clone()) {
                        Ok(v) => v,
                        Err(e) => return format!("Error parsing violations: {}", e),
                    };
                report::report_violations(violations, &mut state.violations).await
            }
            _ => return format!("Unknown tool: {}", tool_call.function.name),
        };

        match result {
            Ok(content) => content,
            Err(error) => format!("Error: {}", error),
        }
    }
}
