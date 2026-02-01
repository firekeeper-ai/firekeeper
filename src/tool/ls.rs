use tiny_loop::tool::tool;

/// List directory contents with optional recursive depth
#[tool]
pub async fn ls(
    /// List directory contents with optional recursive depth
    path: String,
    /// Optional recursion depth (0 for non-recursive)
    depth: Option<usize>,
) -> String {
    let mut items = Vec::new();

    if let Err(e) = list_dir_recursive(&path, depth.unwrap_or(0), 0, "", &mut items).await {
        return format!("Error listing directory: {}", e);
    }

    items.join("\n")
}

fn list_dir_recursive<'a>(
    path: &'a str,
    max_depth: usize,
    current_depth: usize,
    prefix: &'a str,
    items: &'a mut Vec<String>,
) -> std::pin::Pin<Box<dyn std::future::Future<Output = std::io::Result<()>> + Send + 'a>> {
    Box::pin(async move {
        let mut entries = tokio::fs::read_dir(path).await?;
        let mut entry_list = Vec::new();

        while let Some(entry) = entries.next_entry().await? {
            entry_list.push(entry);
        }
        entry_list.sort_by_key(|e| e.file_name());

        for entry in entry_list {
            let file_type = entry.file_type().await?;
            let name = entry.file_name();
            let name_str = name.to_string_lossy();

            let type_prefix = if file_type.is_dir() { "d" } else { "f" };
            items.push(format!("{}{} {}", prefix, type_prefix, name_str));

            if file_type.is_dir() && current_depth < max_depth {
                let new_path = entry.path();
                if let Some(path_str) = new_path.to_str() {
                    list_dir_recursive(
                        path_str,
                        max_depth,
                        current_depth + 1,
                        &format!("{}  ", prefix),
                        items,
                    )
                    .await?;
                }
            }
        }

        Ok(())
    })
}
