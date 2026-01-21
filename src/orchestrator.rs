use crate::rule::body::RuleBody;
use crate::worker;
use futures::future::join_all;
use globset::{Glob, GlobSetBuilder};
use std::collections::HashMap;
use std::process::Command;
use tracing::{debug, error, info, trace, warn};

/// Orchestrate and run code review tasks
///
/// This function coordinates the entire review process:
/// - Resolves the base commit for comparison
/// - Gets changed files and generates diffs
/// - Splits work into tasks based on rules and file scopes
/// - Executes workers in parallel (with optional concurrency limit)
/// - Collects and outputs results
pub async fn orchestrate_and_run(
    rules: &[RuleBody],
    diff_base: &str,
    max_files_per_task: usize,
    max_parallel_workers: Option<usize>,
    base_url: &str,
    api_key: &str,
    model: &str,
    dry_run: bool,
    output: Option<&str>,
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
    let futures: Vec<_> = tasks
        .into_iter()
        .map(|(rule, files)| worker::worker(rule, files, base_url, api_key, model, diffs.clone()))
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
            error!("Task {} failed: {}", i, e);
        } else {
            debug!("Task {} completed successfully", i);
        }
    }

    let failed = results.iter().filter(|r| r.is_err()).count();
    let succeeded = results.len() - failed;
    info!(
        "Review complete: {} succeeded, {} failed",
        succeeded, failed
    );
    
    // Group by file, then by rule
    let mut violations_by_file: HashMap<String, HashMap<String, Vec<String>>> = HashMap::new();
    for result in results {
        if let Ok((rule_name, violations)) = result {
            for violation in violations {
                violations_by_file
                    .entry(violation.file)
                    .or_insert_with(HashMap::new)
                    .entry(rule_name.clone())
                    .or_insert_with(Vec::new)
                    .push(violation.detail);
            }
        }
    }
    
    // Output results
    if let Some(output_path) = output {
        write_output(output_path, &violations_by_file);
    } else {
        print_violations(&violations_by_file);
    }
}

fn print_violations(violations_by_file: &HashMap<String, HashMap<String, Vec<String>>>) {
    if violations_by_file.is_empty() {
        info!("No violations found");
        return;
    }
    
    for line in format_violations(violations_by_file).lines() {
        info!("{}", line);
    }
}

fn write_output(path: &str, violations_by_file: &HashMap<String, HashMap<String, Vec<String>>>) {
    let content = if path.ends_with(".json") {
        serde_json::to_string_pretty(violations_by_file).unwrap()
    } else if path.ends_with(".md") {
        format_violations(violations_by_file)
    } else {
        error!("Output file must end with .md or .json");
        std::process::exit(1);
    };
    
    if let Err(e) = std::fs::write(path, content) {
        error!("Failed to write output file: {}", e);
        std::process::exit(1);
    }
    
    info!("Results written to {}", path);
}

fn format_violations(violations_by_file: &HashMap<String, HashMap<String, Vec<String>>>) -> String {
    if violations_by_file.is_empty() {
        return "No violations found".to_string();
    }
    
    let mut output = String::new();
    for (file, rules) in violations_by_file {
        output.push_str(&format!("# Violations in {}\n", file));
        for (rule, details) in rules {
            output.push_str(&format!("## Rule: {}\n", rule));
            for detail in details {
                output.push_str(&format!("- {}\n", detail));
            }
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

    let diff_base = if base == "ROOT" {
        // Git empty tree hash - a stable constant representing an empty tree object.
        // This SHA-1 hash will never change as it's the result of hashing an empty tree.
        "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
    } else {
        base
    };

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
