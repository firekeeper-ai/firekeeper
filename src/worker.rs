use crate::tool::diff::Diff;
use crate::tool::report::Report;
use crate::{rule::body::RuleBody, types::Violation};
use std::collections::HashMap;
use tiny_loop::{Agent, types::Message};
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
    pub tip: Option<String>,
}

/// Run a review worker for a specific rule and set of files
///
/// Returns a WorkerResult containing violations found and optionally the agent conversation trace
pub async fn worker(
    worker_id: String,
    rule: &RuleBody,
    files: Vec<String>,
    base_url: &str,
    api_key: &str,
    model: &str,
    temperature: Option<f32>,
    max_tokens: u32,
    diffs: HashMap<String, String>,
    trace_enabled: bool,
) -> Result<WorkerResult, Box<dyn std::error::Error>> {
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

    // Setup stateful tools
    let report = Report::new();
    let diff = Diff::new(diffs);

    // Create agent with tools
    let mut agent = Agent::new(llm)
        .system("You are a code reviewer. Your task is to review code changes against a specific rule. \
                Focus only on the files provided and only check for violations of the given rule. \
                You can read related files if needed, but only report issues related to the provided files and rule. \
                \n\nWorkflow:\n\
                1. Get diffs for the provided files to see what changed\n\
                2. Search/read related files if needed for context\n\
                3. Use the 'think' tool to reason about whether the changes violate the rule\n\
                4. Use the 'report' tool to report all violations found, then exit without summary")
        .tool(tiny_loop::tool::read)
        .tool(tiny_loop::tool::fetch)
        .tool(crate::tool::fs::ls)
        .tool(crate::tool::fs::rg)
        .tool(crate::tool::fs::glob)
        .tool(crate::tool::think::think)
        .bind(diff.clone(), Diff::diff)
        .bind(report.clone(), Report::report);

    // User message with rule and files
    let files_list = files.join("\n- ");
    let user_message = format!(
        "Review the following files:\n\n\
        - {}\n\n\
        Against this rule:\n\n\
        <rule>\n{}\n</rule>",
        files_list,
        rule.instruction.trim()
    );
    trace!(
        "[Worker {}] Adding user message with {} files",
        worker_id,
        files.len()
    );
    trace!("[Worker {}] User message: {}", worker_id, user_message);

    // Run agent loop
    debug!(
        "[Worker {}] Starting agent loop for rule '{}'",
        worker_id, rule.name
    );
    let _response = agent.chat(user_message).await?;

    // For trace, we need to collect messages from agent's history
    let messages = if trace_enabled {
        Some(agent.history.get_all().to_vec())
    } else {
        None
    };

    // Extract violations from shared state
    let violations = report.violations.lock().await.clone();

    info!("[Worker {}] Done reviewing rule '{}'", worker_id, rule.name);

    Ok(WorkerResult {
        worker_id,
        rule_name: rule.name.clone(),
        rule_instruction: rule.instruction.clone(),
        files,
        blocking: rule.blocking,
        violations,
        messages,
        tip: rule.tip.clone(),
    })
}
