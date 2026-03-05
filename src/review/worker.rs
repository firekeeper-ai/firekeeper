mod acp;
mod builtin;

use crate::config::AgentConfig;
use crate::review::render::get_fence_backticks;
use crate::{rule::body::RuleBody, types::Violation};
use std::collections::HashMap;
use std::sync::Arc;
use tiny_loop::types::{TimedMessage, ToolDefinition};
use tokio::sync::Mutex;
use tracing::{info, warn};

/// Polling interval for checking shutdown flag during agent chat (milliseconds)
const SHUTDOWN_POLL_INTERVAL_MS: u64 = 100;

/// System message for code review agents
const SYSTEM_MESSAGE: &str = r"You are a code reviewer. Your task is to review code changes against a specific rule.
Focus only on the files provided and only check for violations of the given rule.
You can read related files if needed, but only report issues related to the provided files and rule.

Workflow:
1. Review the provided diffs to understand what changed
2. Read other related diffs or files if needed for context
3. Use the 'think' tool to reason about whether the changes violate the rule
4. Use the 'report' tool to report all violations found, then exit without summary";

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
            load_file_resource(pattern, &mut content, &mut loaded_files);
        } else if let Some(pattern) = resource.strip_prefix("skill://") {
            load_skill_resource(pattern, &mut content, &mut loaded_files);
        } else if let Some(cmd) = resource.strip_prefix("sh://") {
            load_shell_resource(cmd, &mut content).await;
        } else {
            warn!("Unknown resource type: {}", resource);
        }
    }
    content
}

/// Find files matching a glob pattern
fn find_files_by_glob(pattern: &str) -> Vec<String> {
    let (base_path, glob_pattern) = resolve_path(pattern);
    let Ok(glob) = globset::Glob::new(&glob_pattern) else {
        warn!("Invalid glob pattern '{}'", pattern);
        return vec![];
    };

    let mut builder = globset::GlobSetBuilder::new();
    builder.add(glob);
    let Ok(globset) = builder.build() else {
        return vec![];
    };

    let mut matches = Vec::new();
    let _ = glob_recursive(&base_path, &globset, &mut matches, 0);
    matches
}

fn glob_recursive(
    path: &std::path::Path,
    globset: &globset::GlobSet,
    matches: &mut Vec<String>,
    depth: usize,
) -> std::io::Result<()> {
    for entry in std::fs::read_dir(path)? {
        let entry = entry?;
        let entry_path = entry.path();

        if entry_path.is_file() {
            if let Some(path_str) = entry_path.to_str() {
                let relative = path_str.strip_prefix("./").unwrap_or(path_str);
                if globset.is_match(path_str) || globset.is_match(relative) {
                    matches.push(path_str.to_string());
                }
            }
        }

        if entry_path.is_dir() {
            glob_recursive(&entry_path, globset, matches, depth + 1)?;
        }
    }

    Ok(())
}

/// Load file:// resources
fn load_file_resource(
    pattern: &str,
    content: &mut String,
    loaded_files: &mut std::collections::HashSet<String>,
) {
    for path in find_files_by_glob(pattern) {
        if !loaded_files.insert(path.clone()) {
            continue;
        }
        match std::fs::read_to_string(&path) {
            Ok(file_content) => {
                let lang = std::path::Path::new(&path)
                    .extension()
                    .and_then(|s| s.to_str())
                    .unwrap_or("");
                let fence = get_fence_backticks(&file_content);
                content.push_str(&format!(
                    "### {}\n\n{}{}\n{}\n{}\n\n",
                    path, fence, lang, file_content, fence
                ));
            }
            Err(e) => warn!("Failed to read file {}: {}", path, e),
        }
    }
}

/// Load skill:// resources
fn load_skill_resource(
    pattern: &str,
    content: &mut String,
    loaded_files: &mut std::collections::HashSet<String>,
) {
    for path in find_files_by_glob(pattern) {
        if !loaded_files.insert(path.clone()) || !path.ends_with(".md") {
            continue;
        }
        let Ok(file_content) = std::fs::read_to_string(&path) else {
            warn!("Failed to read file {}", path);
            continue;
        };

        let matter = gray_matter::Matter::<gray_matter::engine::YAML>::new();
        let Ok(parsed) = matter.parse::<serde_json::Value>(&file_content) else {
            warn!("Failed to parse frontmatter in {}", path);
            continue;
        };

        if let Some(data) = parsed.data {
            if let Ok(yaml) = serde_yaml_ng::to_string(&data) {
                let fence = get_fence_backticks(&yaml);
                content.push_str(&format!(
                    "### {}\n\nOnly frontmatter loaded. To enable the skill, read the whole md file.\n\n{}yaml\n{}\n{}\n\n",
                    path, fence, yaml, fence
                ));
            }
        }
    }
}

/// Load sh:// resources
async fn load_shell_resource(cmd: &str, content: &mut String) {
    match crate::tool::sh::execute_shell_command(cmd).await {
        Ok(stdout) => {
            let fence = get_fence_backticks(&stdout);
            content.push_str(&format!(
                "### `{}`\n\n{}\n{}\n{}\n\n",
                cmd, fence, stdout, fence
            ));
        }
        Err(e) => warn!("Failed to execute command 'sh://{}': {}", cmd, e),
    }
}

/// Worker result containing violations and optional trace messages
pub struct WorkerResult {
    pub worker_id: String,
    pub rule: RuleBody,
    pub files: Vec<String>,
    pub blocking: bool,
    pub violations: Vec<Violation>,
    pub messages: Option<Vec<TimedMessage>>,
    pub tools: Option<Vec<ToolDefinition>>,
    pub elapsed_secs: f64,
}

/// Build diffs section for focused files
fn build_diffs_section(files: &[String], diffs: &HashMap<String, String>) -> String {
    let mut diffs_content = String::new();
    for file in files {
        if crate::util::should_include_diff(file) {
            if let Some(diff) = diffs.get(file) {
                diffs_content.push_str(diff);
                diffs_content.push('\n');
            }
        }
    }
    if diffs_content.is_empty() {
        String::new()
    } else {
        let fence = get_fence_backticks(&diffs_content);
        format!(
            "Here are diffs of focused files (no need to call diff tool on them):\n\n{}diff\n{}\n{}\n\n",
            fence,
            diffs_content.trim(),
            fence
        )
    }
}

/// Build user message: simplified if focus files match all changed files
fn build_user_message(
    files: &[String],
    all_changed_files: &[String],
    commit_messages: &str,
    is_root_base: bool,
    rule_instruction: &str,
    diffs: &HashMap<String, String>,
    resources_content: &str,
) -> String {
    let mut body = String::new();

    // Commit messages section
    if !is_root_base && !commit_messages.is_empty() {
        body.push_str("## Commit Messages\n\n");
        let fence = get_fence_backticks(commit_messages);
        body.push_str(&format!("{}\n{}\n{}\n\n", fence, commit_messages, fence));
    }

    // Files section
    if !is_root_base {
        if files == all_changed_files {
            body.push_str("## Changed Files\n\n");
            for file in files {
                body.push_str(&format!("- {}\n", file));
            }
            body.push('\n');
        } else {
            body.push_str("## All Changed Files\n\n");
            for file in all_changed_files {
                body.push_str(&format!("- {}\n", file));
            }
            body.push('\n');
            body.push_str("## Focus Files\n\n");
            for file in files {
                body.push_str(&format!("- {}\n", file));
            }
            body.push('\n');
            body.push_str("Note: For most cases, only read the focused files.\n\n");
        }
    } else if files != all_changed_files {
        body.push_str("## Focus Files\n\n");
        for file in files {
            body.push_str(&format!("- {}\n", file));
        }
        body.push('\n');
        body.push_str("Note: For most cases, only read the focused files.\n\n");
    }

    // Rule section
    body.push_str("## Rule\n\n");
    let fence = get_fence_backticks(rule_instruction);
    body.push_str(&format!(
        "{}md\n{}\n{}\n\n",
        fence,
        rule_instruction.trim(),
        fence
    ));

    // Diffs section
    body.push_str("## Diffs\n\n");
    body.push_str(&build_diffs_section(files, diffs));

    // Resources section
    if !resources_content.is_empty() {
        body.push_str("## Resources\n\n");
        body.push_str(resources_content);
    }

    body
}

/// Log worker completion status
fn log_completion(cancelled: bool, worker_id: &str, rule_name: &str, elapsed: f64) {
    if cancelled {
        info!(
            "[Worker {}] Cancelled reviewing rule '{}' ({:.2}s) - returning partial results",
            worker_id, rule_name, elapsed
        );
    } else {
        info!(
            "[Worker {}] Done reviewing rule '{}' ({:.2}s)",
            worker_id, rule_name, elapsed
        );
    }
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
    agent_config: &AgentConfig,
    api_key: &str,
    context_server_url: Option<&str>,
    diffs: HashMap<String, String>,
    trace_enabled: bool,
    shutdown: Arc<Mutex<bool>>,
    is_root_base: bool,
    global_resources: Vec<String>,
    allowed_shell_commands: Vec<String>,
    timeout_secs: u64,
) -> Result<WorkerResult, Box<dyn std::error::Error>> {
    let start = std::time::Instant::now();
    info!(
        "[Worker {}] Reviewing {} files for rule '{}': {:?}",
        worker_id,
        files.len(),
        rule.name,
        files
    );

    match agent_config {
        AgentConfig::Builtin { llm, .. } => {
            builtin::worker_builtin(
                worker_id,
                rule,
                files,
                all_changed_files,
                commit_messages,
                &llm.base_url,
                api_key,
                &llm.model,
                llm.headers.clone(),
                llm.body.clone(),
                diffs,
                trace_enabled,
                shutdown,
                is_root_base,
                global_resources,
                allowed_shell_commands,
                timeout_secs,
                start,
            )
            .await
        }
        AgentConfig::Acp {
            command,
            args,
            mode,
            env,
            ..
        } => {
            acp::worker_acp(
                worker_id,
                rule,
                files,
                all_changed_files,
                commit_messages,
                command,
                args,
                mode,
                env,
                context_server_url.expect("Context server URL required for ACP agents"),
                diffs,
                trace_enabled,
                shutdown,
                is_root_base,
                global_resources,
                timeout_secs,
                start,
            )
            .await
        }
    }
}
