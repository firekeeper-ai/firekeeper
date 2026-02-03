use crate::tool::diff::Diff;
use crate::tool::report::Report;
use crate::{rule::body::RuleBody, types::Violation};
use serde_json::Value;
use std::collections::HashMap;
use std::sync::Arc;
use tiny_loop::Agent;
use tiny_loop::types::{Message, ToolDefinition};
use tokio::sync::Mutex;
use tracing::{debug, info, trace, warn};

/// Polling interval for checking shutdown flag during agent chat (milliseconds)
const SHUTDOWN_POLL_INTERVAL_MS: u64 = 100;

/// Resolve path with ~ and absolute path support, returns (base_path, glob_pattern)
fn resolve_path(pattern: &str) -> (std::path::PathBuf, String) {
    if let Some(rest) = pattern.strip_prefix("~/") {
        if let Some(home) = std::env::var_os("HOME") {
            (std::path::PathBuf::from(home), rest.to_string())
        } else {
            (std::path::PathBuf::from("."), pattern.to_string())
        }
    } else if pattern.starts_with('/') {
        ("/".into(), pattern[1..].to_string())
    } else {
        (std::path::PathBuf::from("."), pattern.to_string())
    }
}

/// Load resources from file://, skill://, or sh:// URIs
async fn load_resources(resources: &[String]) -> String {
    let mut content = String::new();
    let mut loaded_files = std::collections::HashSet::new();

    for resource in resources {
        if let Some(pattern) = resource.strip_prefix("file://") {
            let (base_path, glob_pattern) = resolve_path(pattern);
            match globset::Glob::new(&glob_pattern) {
                Ok(glob) => {
                    let mut builder = globset::GlobSetBuilder::new();
                    builder.add(glob);
                    if let Ok(globset) = builder.build() {
                        let mut matches = Vec::new();
                        let _ = crate::tool::glob::glob_recursive(&base_path, &globset, &mut matches, 0);
                        for path in matches {
                            if loaded_files.insert(path.clone()) {
                                match std::fs::read_to_string(&path) {
                                    Ok(file_content) => {
                                        content.push_str(&format!(
                                            "\n--- {} ---\n{}\n",
                                            path, file_content
                                        ));
                                    }
                                    Err(e) => warn!("Failed to read file {}: {}", path, e),
                                }
                            }
                        }
                    }
                }
                Err(e) => warn!("Invalid glob pattern '{}': {}", pattern, e),
            }
        } else if let Some(pattern) = resource.strip_prefix("skill://") {
            let (base_path, glob_pattern) = resolve_path(pattern);
            match globset::Glob::new(&glob_pattern) {
                Ok(glob) => {
                    let mut builder = globset::GlobSetBuilder::new();
                    builder.add(glob);
                    if let Ok(globset) = builder.build() {
                        let mut matches = Vec::new();
                        let _ = crate::tool::glob::glob_recursive(&base_path, &globset, &mut matches, 0);
                        for path in matches {
                            if loaded_files.insert(path.clone()) && path.ends_with(".md") {
                                match std::fs::read_to_string(&path) {
                                    Ok(file_content) => {
                                        let matter =
                                            gray_matter::Matter::<gray_matter::engine::YAML>::new();
                                        match matter.parse::<serde_json::Value>(&file_content) {
                                            Ok(parsed) => {
                                                let mut md = String::new();
                                                if let Some(data) = parsed.data {
                                                    if let Some(obj) = data.as_object() {
                                                        if let Some(title) = obj
                                                            .get("title")
                                                            .and_then(|v| v.as_str())
                                                        {
                                                            md.push_str(&format!(
                                                                "# {}\n\n",
                                                                title
                                                            ));
                                                        }
                                                        if let Some(desc) = obj
                                                            .get("description")
                                                            .and_then(|v| v.as_str())
                                                        {
                                                            md.push_str(&format!("{}\n", desc));
                                                        }
                                                    }
                                                }
                                                content.push_str(&format!(
                                                    "\n--- {} ---\n{}\n",
                                                    path, md
                                                ));
                                            }
                                            Err(e) => warn!(
                                                "Failed to parse frontmatter in {}: {}",
                                                path, e
                                            ),
                                        }
                                    }
                                    Err(e) => warn!("Failed to read file {}: {}", path, e),
                                }
                            }
                        }
                    }
                }
                Err(e) => warn!("Invalid glob pattern '{}': {}", pattern, e),
            }
        } else if let Some(cmd) = resource.strip_prefix("sh://") {
            #[cfg(windows)]
            let output = tokio::process::Command::new("cmd")
                .arg("/C")
                .arg(cmd)
                .output()
                .await;
            #[cfg(not(windows))]
            let output = tokio::process::Command::new("sh")
                .arg("-c")
                .arg(cmd)
                .output()
                .await;

            match output {
                Ok(output) => {
                    if output.status.success() {
                        content.push_str(&format!(
                            "\n--- sh://{} ---\n{}\n",
                            cmd,
                            String::from_utf8_lossy(&output.stdout)
                        ));
                    } else {
                        warn!("Command failed: sh://{}", cmd);
                    }
                }
                Err(e) => warn!("Failed to execute command 'sh://{}': {}", cmd, e),
            }
        } else {
            warn!("Unknown resource type: {}", resource);
        }
    }
    content
}

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
/// Returns a WorkerResult containing violations found and optionally the agent conversation trace.
/// The worker can be cancelled via the shutdown flag, in which case it returns partial results.
pub async fn worker(
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
    let llm = crate::llm::create_provider(api_key, base_url, model, &headers, &body)?;

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
            format!(
                "Here are diffs of focused files (no need to call diff tool on them):\n\n{}\n\n",
                diffs_content.trim()
            )
        }
    };

    // Create agent with system prompt and bind tools
    let agent = Agent::new(llm)
        .system("You are a code reviewer. Your task is to review code changes against a specific rule. \
                Focus only on the files provided and only check for violations of the given rule. \
                You can read related files if needed, but only report issues related to the provided files and rule. \
                \n\nWorkflow:\n\
                1. Review the provided diffs to understand what changed\n\
                2. Read other related diffs or files if needed for context\n\
                3. Use the 'think' tool to reason about whether the changes violate the rule\n\
                4. Use the 'report' tool to report all violations found, then exit without summary")
        .bind(diff.clone(), Diff::diff)
        .bind(report.clone(), Report::report);

    let mut agent = crate::llm::register_common_tools(agent);

    // Load resources
    let mut all_resources = global_resources.clone();
    all_resources.extend(rule.resources.clone());
    all_resources.sort();
    all_resources.dedup();
    let resources_content = load_resources(&all_resources).await;

    // Build user message: simplified if focus files match all changed files
    let user_message = if files == all_changed_files {
        let files_list = files.join("\n- ");
        let commits_section = if is_root_base || commit_messages.is_empty() {
            String::new()
        } else {
            format!("Commit messages:\n\n{}\n\n", commit_messages)
        };

        let diffs_section = build_diffs_section(&files);

        if is_root_base {
            format!(
                "Rule:\n\n\
                <rule>\n\n{}\n\n</rule>\n\n{}{}",
                rule.instruction.trim(),
                diffs_section,
                resources_content
            )
        } else {
            format!(
                "{}Changed files:\n\n\
                - {}\n\n\
                Rule:\n\n\
                <rule>\n\n{}\n\n</rule>\n\n{}{}",
                commits_section,
                files_list,
                rule.instruction.trim(),
                diffs_section,
                resources_content
            )
        }
    } else {
        // Include all changed files for context, but focus on specific files
        let all_files_list = all_changed_files.join("\n- ");
        let focus_files_list = files.join("\n- ");
        let commits_section = if is_root_base || commit_messages.is_empty() {
            String::new()
        } else {
            format!("Commit messages:\n\n{}\n\n", commit_messages)
        };

        let diffs_section = build_diffs_section(&files);

        if is_root_base {
            format!(
                "Focus on these files:\n\n\
                - {}\n\n\
                Note: For most cases, only read the focused files.\n\n\
                Rule:\n\n\
                <rule>\n\n{}\n\n</rule>\n\n{}{}",
                focus_files_list,
                rule.instruction.trim(),
                diffs_section,
                resources_content
            )
        } else {
            format!(
                "{}All changed files:\n\n\
                - {}\n\n\
                Focus on these files:\n\n\
                - {}\n\n\
                Note: For most cases, only read the focused files.\n\n\
                Rule:\n\n\
                <rule>\n\n{}\n\n</rule>\n\n{}{}",
                commits_section,
                all_files_list,
                focus_files_list,
                rule.instruction.trim(),
                diffs_section,
                resources_content
            )
        }
    };
    trace!(
        "[Worker {}] Adding user message with {} files",
        worker_id,
        files.len()
    );
    trace!("[Worker {}] User message: {}", worker_id, user_message);

    // Run agent loop to review code with cancellation support
    // Uses tokio::select to race between agent chat completion and shutdown signal
    // Polls shutdown flag every 100ms to allow graceful cancellation mid-execution
    debug!(
        "[Worker {}] Starting agent loop for rule '{}'",
        worker_id, rule.name
    );

    let chat_future = agent.chat(user_message);
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

    let cancelled = tokio::select! {
        result = chat_future => {
            result?;
            false
        }
        _ = shutdown_check => {
            warn!("[Worker {}] Cancelled due to shutdown", worker_id);
            true
        }
    };

    // Collect trace data if enabled (even if cancelled)
    let (messages, tools) = if trace_enabled {
        // Collect conversation history and tool schemas for trace output
        (
            Some(agent.history.get_all().to_vec()),
            Some(agent.tools().to_vec()),
        )
    } else {
        (None, None)
    };

    // Extract violations from report tool's shared state
    let violations = report.violations.lock().await.clone();

    let elapsed = start.elapsed().as_secs_f64();

    if cancelled {
        info!(
            "[Worker {}] Cancelled reviewing rule '{}' ({:.2}s) - returning partial results",
            worker_id, rule.name, elapsed
        );
    } else {
        info!(
            "[Worker {}] Done reviewing rule '{}' ({:.2}s)",
            worker_id, rule.name, elapsed
        );
    }

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
