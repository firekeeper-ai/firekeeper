use crate::rule::body::RuleBody;
use crate::types::Violation;
use crate::util;
use crate::worker;
use futures::future::join_all;
use globset::{Glob, GlobSetBuilder};
use serde::Serialize;
use serde_json::Value;
use std::collections::HashMap;
use std::sync::Arc;
use tiny_loop::tool::ToolArgs;
use tiny_loop::types::{Message, ToolDefinition};
use tokio::sync::Mutex;

/// Number of decimal places for elapsed time display
const ELAPSED_TIME_PRECISION: usize = 2;
use tracing::{debug, error, info, trace, warn};

const EXIT_FAILURE: i32 = 1;

/// Trace entry containing worker task details and agent conversation
#[derive(Serialize)]
struct TraceEntry {
    /// Unique identifier for the worker
    worker_id: String,
    /// Name of the rule being checked
    rule_name: String,
    /// Instruction text for the rule
    rule_instruction: String,
    /// List of files being reviewed
    files: Vec<String>,
    /// Time taken to complete the task in seconds
    elapsed_secs: f64,
    /// Tool definitions available to the agent
    tools: Vec<ToolDefinition>,
    /// Conversation messages between agent and tools
    messages: Vec<Message>,
}

/// Orchestrate and run code review tasks
///
/// This function coordinates the entire review process:
/// - Resolves the base commit for comparison
/// - Gets changed files, commit messages, and generates diffs
/// - Splits work into tasks based on rules and file scopes
/// - Executes workers in parallel (with optional concurrency limit)
/// - Collects and outputs results with worker_id, all_files, and commits
/// - Optionally writes trace of agent conversations to file
pub async fn orchestrate_and_run(
    rules: &[RuleBody],
    diff_base: &str,
    max_files_per_task: usize,
    max_parallel_workers: Option<usize>,
    base_url: &str,
    api_key: &str,
    model: &str,
    headers: &HashMap<String, String>,
    body: &Value,
    dry_run: bool,
    output: Option<&str>,
    trace: Option<&str>,
    config_path: &str,
) {
    let base = util::Base::parse(diff_base);
    debug!("Resolved base: {:?}", base);

    debug!("Getting changed files for base");
    let changed_files = util::get_changed_files(&base);
    info!("Found {} changed files", changed_files.len());
    trace!("Changed files: {:?}", changed_files);

    debug!("Generating diffs for {} files", changed_files.len());
    let diffs = util::get_diffs(&base, &changed_files);

    debug!("Getting commit messages for base");
    let commit_messages = util::get_commit_messages(&base);

    debug!(
        "Orchestrating tasks with max_files_per_task: {}",
        max_files_per_task
    );
    let tasks = orchestrate(rules, &changed_files, max_files_per_task);
    let total_tasks = tasks.len();
    info!("Created {} tasks", total_tasks);

    if dry_run {
        info!("Dry run - {} tasks to execute:", tasks.len());
        for (i, (rule, files)) in tasks.iter().enumerate() {
            info!("  Task {}: rule='{}', files={:?}", i, rule.name, files);
        }
        return;
    }

    // Setup Ctrl+C handler for graceful shutdown
    // When triggered, sets shutdown flag that workers poll during execution
    // Workers stop mid-execution and return partial results including trace data
    let shutdown = Arc::new(Mutex::new(false));
    let shutdown_clone = shutdown.clone();
    tokio::spawn(async move {
        tokio::signal::ctrl_c().await.ok();
        warn!("Received Ctrl+C, stopping workers...");
        *shutdown_clone.lock().await = true;
    });

    debug!("Creating worker futures for {} tasks", tasks.len());
    let trace_enabled = trace.is_some();
    let futures: Vec<_> = tasks
        .into_iter()
        .enumerate()
        .map(|(i, (rule, files))| {
            let worker_id = i.to_string();
            let all_files = changed_files.clone();
            let commits = commit_messages.clone();
            let headers = headers.clone();
            let body = body.clone();
            let shutdown_clone = shutdown.clone();
            let is_root = matches!(base, util::Base::Root);
            worker::worker(
                worker_id,
                rule,
                files,
                all_files,
                commits,
                base_url,
                api_key,
                model,
                headers,
                body,
                diffs.clone(),
                trace_enabled,
                shutdown_clone,
                is_root,
            )
        })
        .collect();

    if let Some(max) = max_parallel_workers {
        info!("Running workers with max parallelism: {}", max);
    } else {
        info!("Running workers with unlimited parallelism");
    }

    // Execute workers with optional concurrency limit
    let results = if let Some(max_workers) = max_parallel_workers {
        // Limit parallel execution using a worker pool
        use futures::stream::{FuturesUnordered, StreamExt};
        let mut stream = FuturesUnordered::new();
        let mut results = Vec::new();
        let mut futures_iter = futures.into_iter();

        // Fill initial pool up to max_workers
        for _ in 0..max_workers.min(futures_iter.len()) {
            if let Some(fut) = futures_iter.next() {
                stream.push(fut);
            }
        }

        // As workers complete, spawn new ones to maintain pool size
        // Stop spawning new workers if shutdown is requested
        while let Some(result) = stream.next().await {
            results.push(result);
            if *shutdown.lock().await {
                warn!("Shutdown requested, not spawning new workers");
                break;
            }
            if let Some(fut) = futures_iter.next() {
                stream.push(fut);
            }
        }

        results
    } else {
        // No limit - run all workers in parallel
        join_all(futures).await
    };

    for (i, result) in results.iter().enumerate() {
        if let Err(e) = result {
            error!("[Worker {}] Task failed: {}", i, e);
        } else {
            debug!("[Worker {}] Task completed successfully", i);
        }
    }

    let failed = results.iter().filter(|r| r.is_err()).count();
    let succeeded = results.len() - failed;
    let was_interrupted = *shutdown.lock().await;
    if was_interrupted {
        warn!(
            "Review interrupted: {} succeeded, {} failed, {} cancelled",
            succeeded,
            failed,
            total_tasks - results.len()
        );
    } else {
        info!(
            "Review complete: {} succeeded, {} failed",
            succeeded, failed
        );
    }

    // Group violations by file, then by rule name
    let mut violations_by_file: HashMap<String, HashMap<String, Vec<Violation>>> = HashMap::new();
    let mut tips_by_rule: HashMap<String, String> = HashMap::new();
    let mut blocking_rules_with_violations = std::collections::HashSet::new();
    let mut all_traces = Vec::new();

    for result in results {
        if let Ok(worker_result) = result {
            let has_violations = !worker_result.violations.is_empty();
            for violation in &worker_result.violations {
                violations_by_file
                    .entry(violation.file.clone())
                    .or_insert_with(HashMap::new)
                    .entry(worker_result.rule_name.clone())
                    .or_insert_with(Vec::new)
                    .push(violation.clone());
            }
            if has_violations && worker_result.blocking {
                blocking_rules_with_violations.insert(worker_result.rule_name.clone());
            }
            if let Some(tip) = &worker_result.tip {
                tips_by_rule.insert(worker_result.rule_name.clone(), tip.clone());
            }
            if let Some(messages) = worker_result.messages {
                all_traces.push(TraceEntry {
                    worker_id: worker_result.worker_id,
                    rule_name: worker_result.rule_name,
                    rule_instruction: worker_result.rule_instruction,
                    files: worker_result.files,
                    elapsed_secs: worker_result.elapsed_secs,
                    tools: worker_result.tools.unwrap_or_default(),
                    messages,
                });
            }
        }
    }

    // Output results to file or console
    if let Some(output_path) = output {
        write_output(output_path, &violations_by_file, &tips_by_rule);
    } else {
        print_violations(&violations_by_file, &tips_by_rule);
    }

    // Write trace if enabled
    if let Some(trace_path) = trace {
        write_trace(trace_path, &all_traces);
    }

    // Exit with error if blocking rules have violations
    if !blocking_rules_with_violations.is_empty() {
        error!(
            "Blocking rules with violations: {:?}",
            blocking_rules_with_violations
        );
        info!(
            "If violations are misreported, refine rules in {}",
            config_path
        );
        std::process::exit(EXIT_FAILURE);
    }
}

fn print_violations(
    violations_by_file: &HashMap<String, HashMap<String, Vec<Violation>>>,
    tips_by_rule: &HashMap<String, String>,
) {
    if violations_by_file.is_empty() {
        info!("No violations found");
        return;
    }

    for line in format_violations(violations_by_file, tips_by_rule).lines() {
        info!("{}", line);
    }
}

fn write_output(
    path: &str,
    violations_by_file: &HashMap<String, HashMap<String, Vec<Violation>>>,
    tips_by_rule: &HashMap<String, String>,
) {
    let content = if path.ends_with(".json") {
        let output = serde_json::json!({
            "violations": violations_by_file,
            "tips": tips_by_rule,
        });
        serde_json::to_string_pretty(&output).unwrap()
    } else if path.ends_with(".md") {
        format_violations(violations_by_file, tips_by_rule)
    } else {
        error!("Output file must end with .md or .json");
        std::process::exit(EXIT_FAILURE);
    };

    if let Err(e) = std::fs::write(path, content) {
        error!("Failed to write output file: {}", e);
        std::process::exit(EXIT_FAILURE);
    }

    info!("Results written to {}", path);
}

/// Write trace data to file in JSON or Markdown format
fn write_trace(path: &str, traces: &[TraceEntry]) {
    let content = if path.ends_with(".json") {
        serde_json::to_string_pretty(traces).unwrap()
    } else if path.ends_with(".md") {
        format_trace_markdown(traces)
    } else {
        error!("Trace file must end with .md or .json");
        std::process::exit(EXIT_FAILURE);
    };

    if let Err(e) = std::fs::write(path, content) {
        error!("Failed to write trace file: {}", e);
        std::process::exit(EXIT_FAILURE);
    }

    info!("Trace written to {}", path);
}

/// Format trace entries as Markdown with rule details, files, and agent messages
fn format_trace_markdown(traces: &[TraceEntry]) -> String {
    let mut output = String::new();
    for trace in traces {
        // Write worker and rule header information
        output.push_str(&format!("# Worker: {}\n\n", trace.worker_id));
        output.push_str(&format!("## Rule: {}\n\n", trace.rule_name));
        output.push_str(&format!("{}\n\n", trace.rule_instruction.trim()));
        output.push_str(&format!(
            "**Elapsed:** {:.prec$}s\n\n",
            trace.elapsed_secs,
            prec = ELAPSED_TIME_PRECISION
        ));

        // List files being reviewed
        output.push_str("## Files\n\n");
        for file in &trace.files {
            output.push_str(&format!("- {}\n", file));
        }

        // Show tool schemas as JSON
        output.push_str("\n## Tools\n\n");
        let tools_json = serde_json::to_string_pretty(&trace.tools).unwrap_or_default();
        output.push_str(&format!("```json\n{}\n```\n\n", tools_json));

        // Format conversation messages
        output.push_str("## Messages\n\n");
        for (i, msg) in trace.messages.iter().enumerate() {
            // Extract role, content, and tool calls from message variants
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

            // Format message content with appropriate code fences for tool responses
            if let Some(content) = content {
                if !content.is_empty() {
                    let backticks = get_fence_backticks(content);
                    if role == "tool" {
                        output.push_str(&format!("{}\n{}\n{}\n\n", backticks, content, backticks));
                    } else {
                        output.push_str(&format!("{}\n\n", content));
                    }
                }
            }

            // Format tool calls: special handling for 'think' tool vs others
            if let Some(tool_calls) = tool_calls {
                output.push_str("**Tool Calls:**\n\n");
                for tc in tool_calls {
                    if tc.function.name == crate::tool::think::ThinkArgs::TOOL_NAME {
                        // Render think tool reasoning as markdown
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
                        // Render other tools with JSON arguments
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

fn format_violations(
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
                output.push_str(&format!("\n**Tip:** {}\n", tip));
            }
            output.push('\n');
        }
    }
    output.trim_end().to_string()
}

/// Split rules and files into worker tasks
///
/// For each rule, filters files by scope and splits them into chunks based on
/// max_files_per_task. Returns list of (rule, files) pairs for parallel execution.
fn orchestrate<'a>(
    rules: &'a [RuleBody],
    changed_files: &[String],
    global_max_files_per_task: usize,
) -> Vec<(&'a RuleBody, Vec<String>)> {
    debug!(
        "Orchestrating {} rules against {} files",
        rules.len(),
        changed_files.len()
    );

    rules
        .iter()
        .flat_map(|rule| {
            trace!("Processing rule: {}", rule.name);

            // Filter files that match this rule's scope
            let matched_files = filter_files_by_scope(rule, changed_files);
            debug!("Rule '{}' matched {} files", rule.name, matched_files.len());

            if matched_files.is_empty() {
                return vec![];
            }

            // Use rule-specific or global max_files_per_task
            let max_files = rule.max_files_per_task.unwrap_or(global_max_files_per_task);
            debug!(
                "Rule '{}' using max_files_per_task: {}",
                rule.name, max_files
            );

            // Split matched files into chunks and create tasks
            split_files(&matched_files, max_files)
                .into_iter()
                .map(|chunk| {
                    trace!(
                        "Created chunk with {} files for rule '{}'",
                        chunk.len(),
                        rule.name
                    );
                    (rule, chunk)
                })
                .collect::<Vec<_>>()
        })
        .collect()
}

fn filter_files_by_scope(rule: &RuleBody, files: &[String]) -> Vec<String> {
    let mut builder = GlobSetBuilder::new();
    for pattern in &rule.scope {
        match Glob::new(pattern) {
            Ok(glob) => builder.add(glob),
            Err(e) => {
                warn!(
                    "Invalid glob pattern '{}' in rule '{}': {}",
                    pattern, rule.name, e
                );
                continue;
            }
        };
    }

    let globset = match builder.build() {
        Ok(gs) => gs,
        Err(e) => {
            error!("Failed to build globset for rule '{}': {}", rule.name, e);
            return vec![];
        }
    };

    files
        .iter()
        .filter(|f| globset.is_match(f))
        .cloned()
        .collect()
}

fn split_files(files: &[String], max_per_task: usize) -> Vec<Vec<String>> {
    if files.is_empty() {
        return vec![];
    }

    let total = files.len();
    let num_chunks = (total + max_per_task - 1) / max_per_task;
    let chunk_size = (total + num_chunks - 1) / num_chunks;

    files
        .chunks(chunk_size)
        .map(|chunk| chunk.to_vec())
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_split_files_empty() {
        let files: Vec<String> = vec![];
        let result = split_files(&files, 5);
        assert_eq!(result, Vec::<Vec<String>>::new());
    }

    #[test]
    fn test_split_files_less_than_max() {
        let files: Vec<String> = vec!["a".into(), "b".into(), "c".into()];
        let result = split_files(&files, 5);
        assert_eq!(result.len(), 1);
        assert_eq!(result[0].len(), 3);
    }

    #[test]
    fn test_split_files_exact_max() {
        let files: Vec<String> = vec!["a".into(), "b".into(), "c".into(), "d".into(), "e".into()];
        let result = split_files(&files, 5);
        assert_eq!(result.len(), 1);
        assert_eq!(result[0].len(), 5);
    }

    #[test]
    fn test_split_files_balanced() {
        let files: Vec<String> = (0..7).map(|i| i.to_string()).collect();
        let result = split_files(&files, 5);
        assert_eq!(result.len(), 2);
        assert_eq!(result[0].len(), 4);
        assert_eq!(result[1].len(), 3);
    }

    #[test]
    fn test_split_files_multiple_chunks() {
        let files: Vec<String> = (0..13).map(|i| i.to_string()).collect();
        let result = split_files(&files, 5);
        assert_eq!(result.len(), 3);
        assert_eq!(result[0].len(), 5);
        assert_eq!(result[1].len(), 4);
        assert_eq!(result[2].len(), 4);
    }
}
