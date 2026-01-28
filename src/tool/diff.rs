use std::{collections::HashMap, sync::Arc};
use tiny_loop::tool::tool;
use tracing::warn;

#[derive(Clone)]
pub struct Diff {
    diffs: Arc<HashMap<String, String>>,
}

impl Diff {
    pub fn new(diff: HashMap<String, String>) -> Self {
        Self {
            diffs: Arc::new(diff),
        }
    }
}

#[tool]
impl Diff {
    /// Get git diff for a file. Do NOT use this for lock files (e.g. package-lock.json) or generated files.
    pub async fn diff(
        self,
        /// File path
        path: String,
    ) -> String {
        if path.contains("lock") {
            warn!("trying to get diff for a lock file: {}", path);
        }

        self.diffs
            .get(&path)
            .cloned()
            .unwrap_or_else(|| format!("No diff available for file: {}", path))
    }
}
