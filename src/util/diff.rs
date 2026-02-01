/// Check if a file's diff should be included (excludes lock and generated files)
pub fn should_include_diff(file: &str) -> bool {
    let file_lower = file.to_lowercase();

    // Exclude lock files
    if file_lower.ends_with(".lock") || file_lower.contains("lock.") {
        return false;
    }

    // Exclude generated files
    if file_lower.contains("generated") {
        return false;
    }

    // Exclude common generated files
    if file_lower.ends_with("-lock.json")
        || file_lower.contains(".min.")
        || file_lower.contains("/dist/")
        || file_lower.contains("/build/")
        || file_lower.contains("/target/")
        || file_lower.contains("/.next/")
        || file_lower.contains("/node_modules/")
    {
        return false;
    }

    true
}
