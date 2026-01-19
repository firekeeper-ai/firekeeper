use crate::agent::openai::{OpenAIProvider, Tool, ToolFunction};
use crate::agent::r#loop::AgentLoop;
use crate::fs_tool;
use crate::rule::body::RuleBody;
use serde_json::json;
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
    let tools = create_fs_tools();
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
        {}\n\n\
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

fn create_fs_tools() -> Vec<Tool> {
    vec![
        Tool {
            tool_type: "function".to_string(),
            function: ToolFunction {
                name: "fs_read_file".to_string(),
                description: "Read file contents with optional line range".to_string(),
                parameters: json!({
                    "type": "object",
                    "properties": {
                        "path": {"type": "string", "description": "File path"},
                        "start_line": {"type": "integer", "description": "Optional start line (1-indexed)"},
                        "end_line": {"type": "integer", "description": "Optional end line (inclusive)"}
                    },
                    "required": ["path"]
                }),
            },
        },
        Tool {
            tool_type: "function".to_string(),
            function: ToolFunction {
                name: "fs_list_dir".to_string(),
                description: "List directory contents with optional recursive depth".to_string(),
                parameters: json!({
                    "type": "object",
                    "properties": {
                        "path": {"type": "string", "description": "Directory path"},
                        "depth": {"type": "integer", "description": "Optional recursion depth (0 for non-recursive)"}
                    },
                    "required": ["path"]
                }),
            },
        },
        Tool {
            tool_type: "function".to_string(),
            function: ToolFunction {
                name: "fs_grep".to_string(),
                description: "Search for regex pattern in a file using ripgrep".to_string(),
                parameters: json!({
                    "type": "object",
                    "properties": {
                        "path": {"type": "string", "description": "File path"},
                        "pattern": {"type": "string", "description": "Regex pattern"}
                    },
                    "required": ["path", "pattern"]
                }),
            },
        },
        Tool {
            tool_type: "function".to_string(),
            function: ToolFunction {
                name: "fs_glob_files".to_string(),
                description: "Find files matching a glob pattern".to_string(),
                parameters: json!({
                    "type": "object",
                    "properties": {
                        "path": {"type": "string", "description": "Directory path to search"},
                        "pattern": {"type": "string", "description": "Glob pattern (e.g., **/*.rs)"}
                    },
                    "required": ["path", "pattern"]
                }),
            },
        },
    ]
}

struct FsToolExecutor;

impl crate::agent::r#loop::ToolExecutor for FsToolExecutor {
    fn execute(&self, tool_call: &crate::agent::openai::ToolCall) -> String {
        let args: serde_json::Value = match serde_json::from_str(&tool_call.function.arguments) {
            Ok(v) => v,
            Err(e) => return format!("Error parsing arguments: {}", e),
        };
        
        let operation = match tool_call.function.name.as_str() {
            "fs_read_file" => fs_tool::FsOperation::ReadFile {
                path: args["path"].as_str().unwrap_or("").to_string(),
                start_line: args["start_line"].as_u64().map(|v| v as usize),
                end_line: args["end_line"].as_u64().map(|v| v as usize),
            },
            "fs_list_dir" => fs_tool::FsOperation::ListDir {
                path: args["path"].as_str().unwrap_or("").to_string(),
                depth: args["depth"].as_u64().map(|v| v as usize),
            },
            "fs_grep" => fs_tool::FsOperation::Grep {
                path: args["path"].as_str().unwrap_or("").to_string(),
                pattern: args["pattern"].as_str().unwrap_or("").to_string(),
            },
            "fs_glob_files" => fs_tool::FsOperation::GlobFiles {
                path: args["path"].as_str().unwrap_or("").to_string(),
                pattern: args["pattern"].as_str().unwrap_or("").to_string(),
            },
            _ => return format!("Unknown tool: {}", tool_call.function.name),
        };
        
        let result = fs_tool::execute(&operation);
        if result.success {
            result.content
        } else {
            format!("Error: {}", result.content)
        }
    }
}
