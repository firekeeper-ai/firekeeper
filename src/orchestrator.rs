use crate::rule::body::RuleBody;
use crate::types::Violation;
use crate::worker;
use futures::future::join_all;
use globset::{Glob, GlobSetBuilder};
use serde::Serialize;
use std::collections::HashMap;
use std::process::Command;
use tiny_loop::tool::ToolArgs;
use tiny_loop::types::Message;
use tracing::{debug, error, info, trace, warn};

const EXIT_FAILURE: i32 = 1;

/// Trace entry containing worker task details and agent conversation
#[derive(Serialize)]
struct TraceEntry {
    worker_id: String,
    rule_name: String,
    rule_instruction: String,
    files: Vec<String>,
    messages: Vec<Message>,
}
const GIT_EMPTY_TREE: &str = "4b825dc642cb6eb9a060e54bf8d69288fbee4904";

/// Orchestrate and run code review tasks
///
/// This function coordinates the entire review process:
/// - Resolves the base commit for comparison
/// - Gets changed files and generates diffs
/// - Splits work into tasks based on rules and file scopes
/// - Executes workers in parallel (with optional concurrency limit)
/// - Collects and outputs results
/// - Optionally writes trace of agent conversations to file
pub async fn orchestrate_and_run(
    rules: &[RuleBody],
    diff_base: &str,
    max_files_per_task: usize,
    max_parallel_workers: Option<usize>,
    base_url: &str,
    api_key: &str,
    model: &str,
    temperature: Option<f32>,
    max_tokens: u32,
    dry_run: bool,
    output: Option<&str>,
    trace: Option<&str>,
) {
    let base = resolve_base(diff_base);
    debug!("Resolved base: {}", base);

    debug!("Getting changed files for base: {}", base);
    let changed_files = get_changed_files(&base);
    info!("Found {} changed files", changed_files.len());
    trace!("Changed files: {:?}", changed_files);

    debug!("Generating diffs for {} files", changed_files.len());
    let diffs = get_diffs(&base, &changed_files);

    debug!(
        "Orchestrating tasks with max_files_per_task: {}",
        max_files_per_task
    );
    let tasks = orchestrate(rules, &changed_files, max_files_per_task);
    info!("Created {} tasks", tasks.len());

    if dry_run {
        info!("Dry run - {} tasks to execute:", tasks.len());
        for (i, (rule, files)) in tasks.iter().enumerate() {
            info!("  Task {}: rule='{}', files={:?}", i, rule.name, files);
        }
        return;
    }

    debug!("Creating worker futures for {} tasks", tasks.len());
    let trace_enabled = trace.is_some();
    let futures: Vec<_> = tasks
        .into_iter()
        .enumerate()
        .map(|(i, (rule, files))| {
            let worker_id = i.to_string();
            worker::worker(
                worker_id,
                rule,
                files,
                base_url,
                api_key,
                model,
                temperature,
                max_tokens,
                diffs.clone(),
                trace_enabled,
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
        while let Some(result) = stream.next().await {
            results.push(result);
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
    info!(
        "Review complete: {} succeeded, {} failed",
        succeeded, failed
    );

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
        output.push_str(&format!("# Worker: {}\n\n", trace.worker_id));
        output.push_str(&format!("## Rule: {}\n\n", trace.rule_name));
        output.push_str(&format!(
            "## Rule Instruction\n\n{}\n\n",
            trace.rule_instruction
        ));
        output.push_str(&format!("## Files\n\n"));
        for file in &trace.files {
            output.push_str(&format!("- {}\n", file));
        }
        output.push_str("\n## Messages\n\n");
        for (i, msg) in trace.messages.iter().enumerate() {
            let (role, content, tool_calls) = match msg {
                Message::System { content } => ("system", Some(content.as_str()), None),
                Message::User { content } => ("user", Some(content.as_str()), None),
                Message::Assistant {
                    content,
                    tool_calls,
                } => ("assistant", Some(content.as_str()), tool_calls.as_ref()),
                Message::Tool { content, .. } => ("tool", Some(content.as_str()), None),
                Message::Custom { role, body } => (
                    role.as_str(),
                    body.get("content").and_then(|v| v.as_str()),
                    None,
                ),
            };
            output.push_str(&format!("### Message {} - Role: {}\n\n", i + 1, role));
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
            if let Some(tool_calls) = tool_calls {
                output.push_str("**Tool Calls:**\n\n");
                for tc in tool_calls {
                    if tc.function.name == crate::tool::think::ThinkArgs::TOOL_NAME {
                        // Handle think tool specially
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
                        // Handle non-think tools with JSON
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
            let matched_files = filter_files_by_scope(rule, changed_files);
            debug!("Rule '{}' matched {} files", rule.name, matched_files.len());

            if matched_files.is_empty() {
                return vec![];
            }

            let max_files = rule.max_files_per_task.unwrap_or(global_max_files_per_task);
            debug!(
                "Rule '{}' using max_files_per_task: {}",
                rule.name, max_files
            );

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

fn resolve_base(diff_base: &str) -> String {
    let base = if diff_base.is_empty() {
        debug!("Base is empty, checking for uncommitted changes");
        let has_uncommitted = Command::new("git")
            .args(["diff", "--quiet", "HEAD"])
            .status()
            .map(|s| !s.success())
            .unwrap_or(false);
        let detected = if has_uncommitted { "HEAD" } else { "^" };
        debug!("Auto-detected base: {}", detected);
        detected
    } else {
        diff_base
    };

    if base.starts_with('~') || base.starts_with('^') {
        format!("HEAD{}", base)
    } else {
        base.to_string()
    }
}

fn get_changed_files(base: &str) -> Vec<String> {
    let output = if base == "ROOT" {
        Command::new("git")
            .args(["ls-files"])
            .output()
            .expect("Failed to execute git ls-files")
    } else {
        Command::new("git")
            .args(["diff", "--name-only", base])
            .output()
            .expect("Failed to execute git diff")
    };

    String::from_utf8_lossy(&output.stdout)
        .lines()
        .map(|s| s.to_string())
        .collect()
}

fn get_diffs(base: &str, files: &[String]) -> HashMap<String, String> {
    let mut diffs = HashMap::new();

    let diff_base = if base == "ROOT" { GIT_EMPTY_TREE } else { base };

    for file in files {
        if let Ok(output) = Command::new("git")
            .args(["diff", diff_base, "--", file])
            .output()
        {
            if output.status.success() {
                let diff = String::from_utf8_lossy(&output.stdout).to_string();
                if !diff.is_empty() {
                    diffs.insert(file.clone(), diff);
                }
            }
        }
    }

    diffs
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
