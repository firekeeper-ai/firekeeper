use std::{collections::HashMap, sync::Arc};
use tiny_loop::tool::tool;

/// Tool for retrieving git diffs of changed files
#[derive(Clone)]
pub struct Diff {
    diffs: Arc<HashMap<String, String>>,
}

impl Diff {
    /// Create a new Diff tool with the provided file diffs
    pub fn new(diff: HashMap<String, String>) -> Self {
        Self {
            diffs: Arc::new(diff),
        }
    }
}

#[tool]
impl Diff {
    /// Get git diff for a file.
    pub async fn diff(
        self,
        /// File path
        path: String,
        /// Force read files that are normally excluded.
        /// These files are usually large and not meaningful to review. (default: false)
        force_read: Option<bool>,
    ) -> String {
        let force = force_read.unwrap_or(false);

        if !force && !crate::util::should_include_diff(&path) {
            return format!(
                "Skipped '{}':\n\
                File is excluded.\n\
                These files are usually large and not meaningful to review.\n\
                Use force_read=true to override if necessary.",
                path
            );
        }

        self.diffs
            .get(&path)
            .cloned()
            .unwrap_or_else(|| format!("No diff available for file: {}", path))
    }
}
