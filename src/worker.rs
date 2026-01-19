use crate::agent::llm::openai::OpenAIProvider;
use crate::agent::r#loop::AgentLoop;
use crate::agent::tool::{fs, web, report};
use crate::agent::types::ToolCall;
use crate::rule::body::RuleBody;
use tracing::{debug, info, trace};

pub struct WorkerState {
    pub violations: Vec<report::Violation>,
}

pub async fn worker(
    rule: &RuleBody,
    files: Vec<String>,
    base_url: &str,
    api_key: &str,
    model: &str,
) -> Result<(), Box<dyn std::error::Error>> {
    info!("Worker: reviewing {} files for rule '{}'", files.len(), rule.name);
    trace!("Files to review: {:?}", files);
    
    debug!("Creating OpenAI provider with model: {}", model);
    let provider = OpenAIProvider::new(base_url.to_string(), api_key.to_string(), model.to_string());
    let mut tools = fs::create_fs_tools();
    tools.extend(web::create_web_tools());
    tools.extend(report::create_report_tools());
    trace!("Created {} tools", tools.len());
    let state = WorkerState {
        violations: Vec::new(),
    };
    let mut agent = AgentLoop::new(provider, ToolExecutor, tools, state);
    
    // System message
    trace!("Adding system message to agent");
    agent.add_message(
        "system",
        "You are a code reviewer. Your task is to review code changes against a specific rule. \
        Focus only on the files provided and only check for violations of the given rule. \
        You can read related files if needed, but only report issues related to the provided files and rule. \
        Use the report tool to report all violations found and then exit without summary."
    );
    
    // User message with rule and files
    let files_list = files.join("\n- ");
    let user_message = format!(
        "Review the following files:\n\n\
        - {}\n\n\
        Against this rule:\n\n\
        <rule>\n{}\n</rule>",
        files_list,
        rule.instruction
    );
    trace!("Adding user message with {} files", files.len());
    trace!("User message: {}", user_message);
    agent.add_message("user", &user_message);
    
    debug!("Starting agent loop for rule '{}'", rule.name);
    let response = agent.run().await?;
    info!("Agent response for rule '{}': {}", rule.name, response);
    
    Ok(())
}

struct ToolExecutor;

impl crate::agent::r#loop::ToolExecutor<WorkerState> for ToolExecutor {
    async fn execute(&mut self, tool_call: &ToolCall, state: &mut WorkerState) -> String {
        let args: serde_json::Value = match serde_json::from_str(&tool_call.function.arguments) {
            Ok(v) => v,
            Err(e) => return format!("Error parsing arguments: {}", e),
        };
        
        let result = match tool_call.function.name.as_str() {
            "read" => fs::read_file(
                args["path"].as_str().unwrap_or(""),
                args["start_line"].as_u64().map(|v| v as usize),
                args["end_line"].as_u64().map(|v| v as usize),
                args["show_line_numbers"].as_bool().unwrap_or(false),
                args["limit"].as_u64().map(|v| v as usize).unwrap_or(1000),
            ).await,
            "ls" => fs::list_dir(
                args["path"].as_str().unwrap_or(""),
                args["depth"].as_u64().map(|v| v as usize),
            ).await,
            "rg" => fs::grep(
                args["path"].as_str().unwrap_or(""),
                args["pattern"].as_str().unwrap_or(""),
            ).await,
            "glob" => fs::glob_files(
                args["path"].as_str().unwrap_or(""),
                args["pattern"].as_str().unwrap_or(""),
            ).await,
            "fetch" => web::fetch(args["url"].as_str().unwrap_or("")).await,
            "report" => {
                let violations: Vec<report::Violation> = match serde_json::from_value(args["violations"].clone()) {
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
