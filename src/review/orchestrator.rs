use super::{render, worker};
use crate::rule::body::RuleBody;
use crate::util;
use futures::future::join_all;
use globset::{Glob, GlobSet, GlobSetBuilder};
use serde_json::Value;
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::Mutex;
use tracing::{debug, error, info, trace, warn};

const EXIT_FAILURE: i32 = 1;

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
    global_resources: &[String],
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

    // Setup signal handlers for graceful shutdown (SIGINT/SIGTERM)
    // When triggered, sets shutdown flag that workers poll during execution
    // Workers stop mid-execution and return partial results including trace data
    let shutdown = Arc::new(Mutex::new(false));
    let shutdown_clone = shutdown.clone();
    tokio::spawn(async move {
        #[cfg(unix)]
        {
            use tokio::signal::unix::{signal, SignalKind};
            let mut sigint = signal(SignalKind::interrupt()).unwrap();
            let mut sigterm = signal(SignalKind::terminate()).unwrap();
            tokio::select! {
                _ = sigint.recv() => warn!("Received SIGINT, stopping workers..."),
                _ = sigterm.recv() => warn!("Received SIGTERM, stopping workers..."),
            }
        }
        #[cfg(not(unix))]
        {
            tokio::signal::ctrl_c().await.ok();
            warn!("Received Ctrl+C, stopping workers...");
        }
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
            let resources = global_resources.to_vec();
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
                resources,
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
    let mut violations_by_file: HashMap<String, HashMap<String, Vec<crate::types::Violation>>> =
        HashMap::new();
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
                all_traces.push(render::TraceEntry {
                    worker_id: worker_result.worker_id,
                    rule_name: worker_result.rule_name,
                    rule_instruction: worker_result.rule_instruction,
                    files: worker_result.files,
                    elapsed_secs: worker_result.elapsed_secs,
                    tools: worker_result
                        .tools
                        .unwrap_or_default()
                        .into_iter()
                        .map(|t| serde_json::to_value(t).unwrap_or_default())
                        .collect(),
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

    // Exit with error if any workers failed
    if failed > 0 {
        error!("{} worker(s) failed", failed);
        std::process::exit(EXIT_FAILURE);
    }
}

fn print_violations(
    violations_by_file: &HashMap<String, HashMap<String, Vec<crate::types::Violation>>>,
    tips_by_rule: &HashMap<String, String>,
) {
    if violations_by_file.is_empty() {
        info!("No violations found");
        return;
    }

    for line in render::format_violations(violations_by_file, tips_by_rule).lines() {
        info!("{}", line);
    }
}

fn write_output(
    path: &str,
    violations_by_file: &HashMap<String, HashMap<String, Vec<crate::types::Violation>>>,
    tips_by_rule: &HashMap<String, String>,
) {
    let content = if path.ends_with(".json") {
        let output = serde_json::json!({
            "violations": violations_by_file,
            "tips": tips_by_rule,
        });
        serde_json::to_string_pretty(&output).unwrap()
    } else if path.ends_with(".md") {
        render::format_violations(violations_by_file, tips_by_rule)
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
fn write_trace(path: &str, traces: &[render::TraceEntry]) {
    let content = if path.ends_with(".json") {
        serde_json::to_string_pretty(traces).unwrap()
    } else if path.ends_with(".md") {
        render::format_trace_markdown(traces)
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

fn build_globset(patterns: &[String], rule_name: &str, pattern_type: &str) -> Option<GlobSet> {
    let mut builder = GlobSetBuilder::new();
    for pattern in patterns {
        match Glob::new(pattern) {
            Ok(glob) => builder.add(glob),
            Err(e) => {
                warn!(
                    "Invalid {} pattern '{}' in rule '{}': {}",
                    pattern_type, pattern, rule_name, e
                );
                continue;
            }
        };
    }
    match builder.build() {
        Ok(gs) => Some(gs),
        Err(e) => {
            error!(
                "Failed to build {} globset for rule '{}': {}",
                pattern_type, rule_name, e
            );
            None
        }
    }
}

fn filter_files_by_scope(rule: &RuleBody, files: &[String]) -> Vec<String> {
    let Some(globset) = build_globset(&rule.scope, &rule.name, "scope") else {
        return vec![];
    };
    let Some(exclude_globset) = build_globset(&rule.exclude, &rule.name, "exclude") else {
        return vec![];
    };

    files
        .iter()
        .filter(|f| globset.is_match(f) && !exclude_globset.is_match(f))
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
        assert_eq!(result[1].len(), 5);
        assert_eq!(result[2].len(), 3);
    }

    #[test]
    fn test_filter_files_by_scope_with_exclude() {
        let rule = RuleBody {
            name: "Test Rule".into(),
            description: "Test".into(),
            instruction: "Test".into(),
            scope: vec!["src/**/*.rs".into()],
            exclude: vec!["**/tests/**".into(), "**/*_test.rs".into()],
            max_files_per_task: None,
            blocking: true,
            tip: None,
            resources: vec![],
        };

        let files = vec![
            "src/main.rs".into(),
            "src/lib.rs".into(),
            "src/tests/helper.rs".into(),
            "src/util_test.rs".into(),
            "src/util.rs".into(),
        ];

        let result = filter_files_by_scope(&rule, &files);
        assert_eq!(result, vec!["src/main.rs", "src/lib.rs", "src/util.rs"]);
    }
}
