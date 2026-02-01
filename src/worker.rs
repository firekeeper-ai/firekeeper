use crate::tool::diff::Diff;
use crate::tool::report::Report;
use crate::{rule::body::RuleBody, types::Violation};
use std::collections::HashMap;
use tiny_loop::Agent;
use tiny_loop::tool::ToolArgs;
use tiny_loop::types::{Message, ToolDefinition};
use tracing::{debug, info, trace};

/// Worker result containing violations and optional trace messages
pub struct WorkerResult {
    pub worker_id: String,
    pub rule_name: String,
    pub rule_instruction: String,
    pub files: Vec<String>,
    pub blocking: bool,
    pub violations: Vec<Violation>,
    pub messages: Option<Vec<Message>>,
    pub tools: Option<Vec<ToolDefinition>>,
    pub tip: Option<String>,
    pub elapsed_secs: f64,
}

/// Run a review worker for a specific rule and set of files
///
/// Returns a WorkerResult containing violations found and optionally the agent conversation trace
pub async fn worker(
    worker_id: String,
    rule: &RuleBody,
    files: Vec<String>,
    all_changed_files: Vec<String>,
    commit_messages: String,
    base_url: &str,
    api_key: &str,
    model: &str,
    temperature: Option<f32>,
    max_tokens: u32,
    diffs: HashMap<String, String>,
    trace_enabled: bool,
) -> Result<WorkerResult, Box<dyn std::error::Error>> {
    let start = std::time::Instant::now();
    info!(
        "[Worker {}] Reviewing {} files for rule '{}': {:?}",
        worker_id,
        files.len(),
        rule.name,
        files
    );

    // Setup LLM provider
    debug!(
        "[Worker {}] Creating OpenAI provider with model: {}",
        worker_id, model
    );
    let llm = crate::llm::create_provider(api_key, base_url, model, temperature, max_tokens);

    // Setup stateful tools for reporting violations and getting diffs
    let report = Report::new();
    let diff = Diff::new(diffs.clone());

    // Helper function to build diffs section for focused files
    let build_diffs_section = |files: &[String]| -> String {
        let mut diffs_content = String::new();
        for file in files {
            if crate::util::should_include_diff(file) {
                if let Some(diff) = diffs.get(file) {
                    diffs_content.push_str(&format!("```diff\n{}\n```\n\n", diff));
                }
            }
        }
        if diffs_content.is_empty() {
            String::new()
        } else {
            format!("<diff>\n\n{}</diff>\n\n", diffs_content.trim_end())
        }
    };

    // Create agent with system prompt and bind tools
    let agent = Agent::new(llm)
        .system("You are a code reviewer. Your task is to review code changes against a specific rule. \
                Focus only on the files provided and only check for violations of the given rule. \
                You can read related files if needed, but only report issues related to the provided files and rule. \
                \n\nWorkflow:\n\
                1. Get diffs for the provided files to see what changed\n\
                2. Search/read related files if needed for context\n\
                3. Use the 'think' tool to reason about whether the changes violate the rule\n\
                4. Use the 'report' tool to report all violations found, then exit without summary")
        .bind(diff.clone(), Diff::diff)
        .bind(report.clone(), Report::report);

    let mut agent = crate::llm::register_common_tools(agent);

    // Build user message: simplified if focus files match all changed files
    let user_message = if files == all_changed_files {
        let files_list = files.join("\n- ");
        let commits_section = if commit_messages.is_empty() {
            String::new()
        } else {
            format!("Commit messages:\n\n{}\n\n", commit_messages)
        };

        let diffs_section = build_diffs_section(&files);

        format!(
            "{}Changed files:\n\n\
            - {}\n\n\
            Rule:\n\n\
            <rule>\n\n{}\n\n</rule>\n\n{}",
            commits_section,
            files_list,
            rule.instruction.trim(),
            diffs_section
        )
    } else {
        // Include all changed files for context, but focus on specific files
        let all_files_list = all_changed_files.join("\n- ");
        let focus_files_list = files.join("\n- ");
        let commits_section = if commit_messages.is_empty() {
            String::new()
        } else {
            format!("Commit messages:\n\n{}\n\n", commit_messages)
        };

        let diffs_section = build_diffs_section(&files);

        format!(
            "{}All changed files:\n\n\
            - {}\n\n\
            Focus on these files:\n\n\
            - {}\n\n\
            Note: For most cases, only read the focused files.\n\n\
            Rule:\n\n\
            <rule>\n{}\n</rule>\n\n{}",
            commits_section,
            all_files_list,
            focus_files_list,
            rule.instruction.trim(),
            diffs_section
        )
    };
    trace!(
        "[Worker {}] Adding user message with {} files",
        worker_id,
        files.len()
    );
    trace!("[Worker {}] User message: {}", worker_id, user_message);

    // Run agent loop to review code
    debug!(
        "[Worker {}] Starting agent loop for rule '{}'",
        worker_id, rule.name
    );
    let _response = agent.chat(user_message).await?;

    // Collect trace data if enabled
    let (messages, tools) = if trace_enabled {
        // Collect conversation history and tool schemas for trace output
        let tool_schemas = vec![
            crate::tool::read::ReadArgs::definition(),
            crate::tool::fetch::FetchArgs::definition(),
            crate::tool::ls::LsArgs::definition(),
            crate::tool::grep::GrepArgs::definition(),
            crate::tool::glob::GlobArgs::definition(),
            crate::tool::think::ThinkArgs::definition(),
            crate::tool::diff::DiffArgs::definition(),
            crate::tool::report::ReportArgs::definition(),
        ];
        (Some(agent.history.get_all().to_vec()), Some(tool_schemas))
    } else {
        (None, None)
    };

    // Extract violations from report tool's shared state
    let violations = report.violations.lock().await.clone();

    let elapsed = start.elapsed().as_secs_f64();
    info!(
        "[Worker {}] Done reviewing rule '{}' ({:.2}s)",
        worker_id, rule.name, elapsed
    );

    Ok(WorkerResult {
        worker_id,
        rule_name: rule.name.clone(),
        rule_instruction: rule.instruction.clone(),
        files,
        blocking: rule.blocking,
        violations,
        messages,
        tools,
        tip: rule.tip.clone(),
        elapsed_secs: elapsed,
    })
}
