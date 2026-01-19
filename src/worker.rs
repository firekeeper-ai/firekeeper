use crate::agent::llm::openai::OpenAIProvider;
use crate::agent::r#loop::AgentLoop;
use crate::agent::tool::fs;
use crate::agent::types::ToolCall;
use crate::rule::body::RuleBody;
use tracing::{debug, info, trace};

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
    let tools = fs::create_fs_tools();
    trace!("Created {} filesystem tools", tools.len());
    let mut agent = AgentLoop::new(provider, FsToolExecutor, tools);
    
    // System message
    trace!("Adding system message to agent");
    agent.add_message(
        "system",
        "You are a code reviewer. Your task is to review code changes against a specific rule. \
        Focus only on the files provided and only check for violations of the given rule. \
        You can read related files if needed, but only comment on issues related to the rule. \
        Provide a clear review result."
    );
    
    // User message with rule and files
    let files_list = files.join("\n- ");
    let user_message = format!(
        "Review the following files against this rule:\n\n\
        <rule>\n{}\n</rule>\n\n\
        Files to review:\n- {}\n\n\
        Please review these files and report any violations of the rule.",
        rule.instruction,
        files_list
    );
    trace!("Adding user message with {} files", files.len());
    trace!("User message: {}", user_message);
    agent.add_message("user", &user_message);
    
    debug!("Starting agent loop for rule '{}'", rule.name);
    let response = agent.run().await?;
    info!("Agent response for rule '{}': {}", rule.name, response);
    
    Ok(())
}

struct FsToolExecutor;

impl crate::agent::r#loop::ToolExecutor for FsToolExecutor {
    fn execute(&self, tool_call: &ToolCall) -> String {
        let args: serde_json::Value = match serde_json::from_str(&tool_call.function.arguments) {
            Ok(v) => v,
            Err(e) => return format!("Error parsing arguments: {}", e),
        };
        
        let result = match tool_call.function.name.as_str() {
            "fs_read_file" => fs::read_file(
                args["path"].as_str().unwrap_or(""),
                args["start_line"].as_u64().map(|v| v as usize),
                args["end_line"].as_u64().map(|v| v as usize),
            ),
            "fs_list_dir" => fs::list_dir(
                args["path"].as_str().unwrap_or(""),
                args["depth"].as_u64().map(|v| v as usize),
            ),
            "fs_grep" => fs::grep(
                args["path"].as_str().unwrap_or(""),
                args["pattern"].as_str().unwrap_or(""),
            ),
            "fs_glob_files" => fs::glob_files(
                args["path"].as_str().unwrap_or(""),
                args["pattern"].as_str().unwrap_or(""),
            ),
            _ => return format!("Unknown tool: {}", tool_call.function.name),
        };
        
        if result.success {
            result.content
        } else {
            format!("Error: {}", result.content)
        }
    }
}
