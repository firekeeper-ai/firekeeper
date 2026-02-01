use crate::config::Config;
use crate::rule::body::RuleBody;
use crate::tool::diff::Diff;
use crate::tool::suggest::Suggest;
use crate::util;
use tiny_loop::Agent;
use tracing::info;

/// Suggest new review rules based on code changes
///
/// Analyzes code changes and uses an LLM agent to suggest new rules that could
/// catch violations in the current changes. Returns suggested rules or error.
pub async fn suggest(
    diff_base: &str,
    config: &Config,
    api_key: &str,
    base_url: &str,
    model: &str,
    _output: Option<&str>,
    _trace: Option<&str>,
) -> Result<(), Box<dyn std::error::Error>> {
    info!("Starting rule suggestion");

    // Determine base commit
    let base = util::Base::parse(diff_base);
    info!("Using base: {:?}", base);

    // Get changed files and diffs
    let changed_files = util::get_changed_files(&base);
    if changed_files.is_empty() {
        info!("No changes found");
        return Ok(());
    }
    info!("Found {} changed files", changed_files.len());

    let diffs = util::get_diffs(&base, &changed_files);
    if diffs.is_empty() {
        info!("No diffs found");
        return Ok(());
    }

    // Load existing rules
    let existing_rules: Vec<String> = config
        .rules
        .iter()
        .map(|r| format!("- {}: {}", r.name, r.instruction))
        .collect();
    let existing_rules_text = if existing_rules.is_empty() {
        "No existing rules.".to_string()
    } else {
        existing_rules.join("\n")
    };

    // Setup LLM provider
    let provider = crate::llm::create_provider(
        api_key,
        base_url,
        model,
        &config.llm.headers,
        &config.llm.body,
    )?;

    // Create agent with tools
    let diff_tool = Diff::new(diffs.clone());
    let suggest_tool = Suggest::new();
    let files: Vec<String> = diffs.keys().cloned().collect();

    let agent = Agent::new(provider)
        .system(&format!(
            "You are a code review expert analyzing code changes to suggest new review rules.\n\n\
            Changed files: {}\n\n\
            Existing rules:\n{}\n\n\
            Your task:\n\
            1. Review all file diffs to understand the changes\n\
            2. Identify patterns that should be enforced as rules\n\
            3. Use the 'think' tool to reason about potential rules\n\
            4. Use the 'suggest' tool to report suggested rules that are:\n\
               - Clear and concise\n\
               - Not duplicated with existing rules\n\
               - Address severe violations that exist in the current changes\n\
               - Have clear success/pass criteria\n\n",
            files.join(", "),
            existing_rules_text
        ))
        .bind(diff_tool, crate::tool::diff::Diff::diff)
        .bind(suggest_tool.clone(), Suggest::suggest);

    let mut agent = crate::llm::register_common_tools(agent);

    info!("Running agent to suggest rules");
    let _response = agent
        .chat("Analyze the code changes and suggest new review rules.")
        .await?;

    // Extract suggested rules
    let suggested_rules = suggest_tool.rules.lock().await.clone();
    info!("Suggested {} rules", suggested_rules.len());

    for rule in &suggested_rules {
        info!("- {}: {}", rule.name, rule.instruction);
    }

    // Save to output file if specified
    if let Some(output_path) = _output {
        write_output(output_path, &suggested_rules)?;
    }

    Ok(())
}

/// Write suggested rules to output file in JSON or Markdown format
fn write_output(path: &str, rules: &[RuleBody]) -> Result<(), Box<dyn std::error::Error>> {
    let content = if path.ends_with(".json") {
        serde_json::to_string_pretty(&rules)?
    } else if path.ends_with(".md") {
        format_rules_markdown(rules)
    } else {
        return Err("Output file must end with .md or .json".into());
    };

    std::fs::write(path, content)?;
    info!("Suggested rules written to {}", path);
    Ok(())
}

/// Format rules as Markdown with name, instruction, scope, and tip
fn format_rules_markdown(rules: &[RuleBody]) -> String {
    let mut output = String::from("# Suggested Rules\n\n");

    for rule in rules {
        output.push_str(&format!("## {}\n\n", rule.name));

        if !rule.description.is_empty() {
            output.push_str(&format!("**Description:** {}\n\n", rule.description));
        }

        output.push_str(&format!("**Instruction:** {}\n\n", rule.instruction));

        if let Some(tip) = &rule.tip {
            output.push_str(&format!("**Tip:** {}\n\n", tip));
        }

        output.push_str(&format!("**Blocking:** {}\n\n", rule.blocking));

        if !rule.scope.is_empty() {
            output.push_str("**Scope:**\n");
            for pattern in &rule.scope {
                output.push_str(&format!("- `{}`\n", pattern));
            }
            output.push('\n');
        }

        output.push_str("---\n\n");
    }

    output
}
