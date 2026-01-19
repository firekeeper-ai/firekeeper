use crate::rule::body::RuleBody;
use crate::worker;
use futures::future::join_all;
use globset::{Glob, GlobSetBuilder};
use std::process::Command;
use tracing::{error, info, warn};

pub async fn orchestrate_and_run(
    rules: &[RuleBody],
    diff_base: &str,
    max_files_per_task: usize,
    max_parallel_workers: Option<usize>,
    base_url: &str,
    api_key: &str,
    model: &str,
    dry_run: bool,
) {
    let changed_files = get_changed_files(diff_base);
    let tasks = orchestrate(rules, &changed_files, max_files_per_task);
    
    if dry_run {
        info!("Dry run - {} tasks to execute:", tasks.len());
        for (i, (rule, files)) in tasks.iter().enumerate() {
            info!("  Task {}: rule='{}', files={:?}", i, rule.name, files);
        }
        return;
    }
    
    let futures: Vec<_> = tasks.into_iter()
        .map(|(rule, files)| worker::worker(rule, files, base_url, api_key, model))
        .collect();
    
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
        }
    }
}

fn orchestrate<'a>(
    rules: &'a [RuleBody],
    changed_files: &[String],
    max_files_per_task: usize,
) -> Vec<(&'a RuleBody, Vec<String>)> {
    rules.iter()
        .flat_map(|rule| {
            let matched_files = filter_files_by_scope(rule, changed_files);
            if matched_files.is_empty() {
                return vec![];
            }
            
            split_files(&matched_files, max_files_per_task)
                .into_iter()
                .map(|chunk| (rule, chunk))
                .collect::<Vec<_>>()
        })
        .collect()
}

fn get_changed_files(diff_base: &str) -> Vec<String> {
    // Auto-detect: if base is empty, check for uncommitted changes
    let base = if diff_base.is_empty() {
        let has_uncommitted = Command::new("git")
            .args(["diff", "--quiet", "HEAD"])
            .status()
            .map(|s| !s.success())
            .unwrap_or(false);
        
        if has_uncommitted { "HEAD" } else { "^" }
    } else {
        diff_base
    };
    
    // Prepend HEAD if base starts with ~ or ^
    let base = if base.starts_with('~') || base.starts_with('^') {
        format!("HEAD{}", base)
    } else {
        base.to_string()
    };
    
    let output = if base == "HEAD" {
        // Review uncommitted changes (working directory vs HEAD)
        Command::new("git")
            .args(["diff", "--name-only", "HEAD"])
            .output()
            .expect("Failed to execute git diff")
    } else {
        // Review committed changes (base..HEAD)
        let diff_range = format!("{}..HEAD", base);
        Command::new("git")
            .args(["diff", "--name-only", &diff_range])
            .output()
            .expect("Failed to execute git diff")
    };
    
    String::from_utf8_lossy(&output.stdout)
        .lines()
        .map(|s| s.to_string())
        .collect()
}

fn filter_files_by_scope(rule: &RuleBody, files: &[String]) -> Vec<String> {
    let mut builder = GlobSetBuilder::new();
    for pattern in &rule.scope {
        match Glob::new(pattern) {
            Ok(glob) => builder.add(glob),
            Err(e) => {
                warn!("Invalid glob pattern '{}' in rule '{}': {}", pattern, rule.name, e);
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
    
    files.iter()
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
    
    files.chunks(chunk_size)
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
