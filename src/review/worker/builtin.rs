use crate::review::worker::{
    SHUTDOWN_POLL_INTERVAL_MS, WorkerResult, build_user_message, load_resources, log_completion,
};
use crate::rule::body::RuleBody;
use crate::tool::diff::Diff;
use crate::tool::report::Report;
use serde_json::Value;
use std::collections::HashMap;
use std::sync::Arc;
use tiny_loop::Agent;
use tiny_loop::tool::ToolArgs;
use tiny_loop::types::{Message, TimedMessage, ToolDefinition, UserMessage};
use tokio::sync::Mutex;
use tracing::{debug, trace, warn};

pub async fn worker_builtin(
    worker_id: String,
    rule: &RuleBody,
    files: Vec<String>,
    all_changed_files: Vec<String>,
    commit_messages: String,
    base_url: &str,
    api_key: &str,
    model: &str,
    headers: HashMap<String, String>,
    body: Value,
    diffs: HashMap<String, String>,
    trace_enabled: bool,
    shutdown: Arc<Mutex<bool>>,
    is_root_base: bool,
    global_resources: Vec<String>,
    allowed_shell_commands: Vec<String>,
    timeout_secs: u64,
    start: std::time::Instant,
) -> Result<WorkerResult, Box<dyn std::error::Error>> {
    // Setup LLM provider
    debug!(
        "[Worker {}] Creating OpenAI provider with model: {}",
        worker_id, model
    );
    let llm = crate::llm::create_provider(api_key, base_url, model, &headers, &body)?;

    // Setup stateful tools for reporting violations and getting diffs
    let report = Report::new();
    let diff = Diff::new(diffs.clone());

    // Create agent with system prompt and bind tools
    let agent = Agent::new(llm)
        .system(super::SYSTEM_MESSAGE)
        .bind(diff.clone(), Diff::diff)
        .bind(report.clone(), Report::report);

    let agent = crate::llm::register_common_tools(agent, &allowed_shell_commands);

    // Load resources
    let mut all_resources = global_resources.clone();
    all_resources.extend(rule.resources.clone());
    all_resources.sort();
    all_resources.dedup();
    let resources_content = load_resources(&all_resources).await;

    // Build user message
    let user_message = build_user_message(
        &files,
        &all_changed_files,
        &commit_messages,
        is_root_base,
        &rule.instruction,
        &diffs,
        &resources_content,
    );
    trace!(
        "[Worker {}] Adding user message with {} files",
        worker_id,
        files.len()
    );
    trace!("[Worker {}] User message: {}", worker_id, user_message);

    // Run agent loop to review code with cancellation support and timeout
    let (cancelled, agent) = run_agent_with_cancellation(
        agent,
        user_message,
        shutdown,
        timeout_secs,
        &worker_id,
        &rule.name,
    )
    .await?;

    // Collect trace data if enabled (even if cancelled)
    let (messages, tools) = collect_trace_data(trace_enabled, &agent);

    // Extract violations from report tool's shared state
    let violations = report.violations.lock().await.clone();

    let elapsed = start.elapsed().as_secs_f64();

    log_completion(cancelled, &worker_id, &rule.name, elapsed);

    Ok(WorkerResult {
        worker_id,
        rule: rule.clone(),
        files,
        blocking: rule.blocking,
        violations,
        messages,
        tools,
        elapsed_secs: elapsed,
    })
}

async fn run_agent_loop(agent: &mut Agent, user_message: String) -> anyhow::Result<()> {
    agent.history.add(TimedMessage {
        message: Message::User(UserMessage {
            content: user_message,
        }),
        timestamp: std::time::SystemTime::now(),
        elapsed: std::time::Duration::ZERO,
    });

    let mut seen_tool_calls = std::collections::HashSet::new();
    let mut seen_report_locations = std::collections::HashSet::new();

    loop {
        if let Some(_) = agent.step().await? {
            return Ok(());
        }

        // Check for empty report
        for timed_msg in agent.history.get_all() {
            if let Message::Assistant(am) = &timed_msg.message {
                if let Some(tool_calls) = &am.tool_calls {
                    for tc in tool_calls {
                        if tc.function.name == crate::tool::report::ReportArgs::TOOL_NAME {
                            if let Ok(args) = serde_json::from_str::<crate::tool::report::ReportArgs>(
                                &tc.function.arguments,
                            ) {
                                if args.violations.is_empty() {
                                    debug!("Early stop due to empty violation");
                                    return Ok(());
                                }
                            }
                        }
                    }
                }
            }
        }

        // Check for duplicated report locations
        if let Some(timed_msg) = agent.history.get_all().last() {
            if let Message::Assistant(am) = &timed_msg.message {
                if let Some(tool_calls) = &am.tool_calls {
                    for tc in tool_calls {
                        if tc.function.name == crate::tool::report::ReportArgs::TOOL_NAME {
                            if let Ok(args) = serde_json::from_str::<crate::tool::report::ReportArgs>(
                                &tc.function.arguments,
                            ) {
                                for v in &args.violations {
                                    let key = format!("{}:{}:{}", v.file, v.start_line, v.end_line);
                                    if !seen_report_locations.insert(key) {
                                        warn!(
                                            "Duplicate report for same location detected, might be dead loop"
                                        );
                                        return Ok(());
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }

        // Check for duplicated tool calls
        if let Some(timed_msg) = agent.history.get_all().last() {
            if let Message::Assistant(am) = &timed_msg.message {
                if let Some(tool_calls) = &am.tool_calls {
                    for tc in tool_calls {
                        let key = format!("{}:{}", tc.function.name, tc.function.arguments);
                        if !seen_tool_calls.insert(key) {
                            debug!("Early stop with duplicated tool call, might be dead loop");
                            return Err(anyhow::anyhow!(
                                "Duplicated tool call detected, might be dead loop"
                            ));
                        }
                    }
                }
            }
        }
    }
}

/// Run agent loop with cancellation support
/// Uses tokio::select to race between agent chat completion and shutdown signal
/// Polls shutdown flag every 100ms to allow graceful cancellation mid-execution
async fn run_agent_with_cancellation(
    mut agent: Agent,
    user_message: String,
    shutdown: Arc<Mutex<bool>>,
    timeout_secs: u64,
    worker_id: &str,
    rule_name: &str,
) -> Result<(bool, Agent), Box<dyn std::error::Error>> {
    debug!(
        "[Worker {}] Starting agent loop for rule '{}'",
        worker_id, rule_name
    );

    let chat_future = run_agent_loop(&mut agent, user_message);
    let shutdown_check = async {
        loop {
            tokio::time::sleep(tokio::time::Duration::from_millis(
                SHUTDOWN_POLL_INTERVAL_MS,
            ))
            .await;
            if *shutdown.lock().await {
                break;
            }
        }
    };
    let timeout_future = tokio::time::sleep(tokio::time::Duration::from_secs(timeout_secs));

    let cancelled = tokio::select! {
        result = chat_future => {
            result?;
            false
        }
        _ = shutdown_check => {
            warn!("[Worker {}] Cancelled due to shutdown", worker_id);
            true
        }
        _ = timeout_future => {
            warn!("[Worker {}] Timeout after {}s", worker_id, timeout_secs);
            true
        }
    };

    Ok((cancelled, agent))
}

/// Collect trace data if enabled
fn collect_trace_data(
    trace_enabled: bool,
    agent: &Agent,
) -> (Option<Vec<TimedMessage>>, Option<Vec<ToolDefinition>>) {
    if trace_enabled {
        // Collect conversation history and tool schemas for trace output
        (
            Some(agent.history.get_all().to_vec()),
            Some(agent.tools().to_vec()),
        )
    } else {
        (None, None)
    }
}
