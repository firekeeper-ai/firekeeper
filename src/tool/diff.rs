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
    /// Get git diff for files.
    pub async fn diff(
        self,
        /// File paths
        path: Vec<String>,
        /// Force read files that are normally excluded.
        /// These files are usually large and not meaningful to review. (default: false)
        force_read: Option<bool>,
    ) -> String {
        if path.len() == 1 {
            return self.diff_one(&path[0], force_read);
        }

        path.iter()
            .map(|p| format!("=== {} ===\n{}", p, self.diff_one(p, force_read)))
            .collect::<Vec<_>>()
            .join("\n\n")
    }
}

impl Diff {
    fn diff_one(&self, path: &str, force_read: Option<bool>) -> String {
        let force = force_read.unwrap_or(false);

        if !force && !crate::util::should_include_diff(path) {
            return format!(
                "Skipped '{}':\n\
                File is excluded.\n\
                These files are usually large and not meaningful to review.\n\
                Use force_read=true to override if necessary.",
                path
            );
        }

        self.diffs
            .get(path)
            .cloned()
            .unwrap_or_else(|| format!("No diff available for file: {}", path))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_diff_one_existing_file() {
        let mut diffs = HashMap::new();
        diffs.insert("file.rs".to_string(), "diff content".to_string());
        let diff = Diff::new(diffs);

        let result = diff.diff_one("file.rs", None);
        assert_eq!(result, "diff content");
    }

    #[test]
    fn test_diff_one_missing_file() {
        let diff = Diff::new(HashMap::new());

        let result = diff.diff_one("missing.rs", None);
        assert_eq!(result, "No diff available for file: missing.rs");
    }

    #[test]
    fn test_diff_one_excluded_file() {
        let mut diffs = HashMap::new();
        diffs.insert("package-lock.json".to_string(), "diff".to_string());
        let diff = Diff::new(diffs);

        let result = diff.diff_one("package-lock.json", None);
        assert!(result.contains("Skipped"));
        assert!(result.contains("excluded"));
    }

    #[test]
    fn test_diff_one_excluded_file_with_force() {
        let mut diffs = HashMap::new();
        diffs.insert("package-lock.json".to_string(), "diff".to_string());
        let diff = Diff::new(diffs);

        let result = diff.diff_one("package-lock.json", Some(true));
        assert_eq!(result, "diff");
    }
}
