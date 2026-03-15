//! Plugin dispatch — spawn the runtime, pipe JSON in, read JSON out.

use crate::config::{Registry, ResolvedCommand};
use anyhow::{bail, Context, Result};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::process::Stdio;
use tokio::io::AsyncWriteExt;
use tokio::process::Command;

// ── Wire protocol ─────────────────────────────────────────────────────────────

/// Sent to the plugin via stdin as a single JSON line.
#[derive(Debug, Serialize)]
pub struct PluginRequest {
    pub command: String,
    pub args: Vec<String>,
    pub env: HashMap<String, String>,
}

/// Received from the plugin via stdout as a single JSON object.
#[derive(Debug, Deserialize)]
pub struct PluginResponse {
    /// "success" or "error"
    pub status: String,
    /// Human-readable output for display in the TUI / stdout
    pub output: Option<String>,
    /// Structured data for rich TUI rendering (optional)
    pub data: Option<serde_json::Value>,
    /// Error message when status == "error"
    pub error: Option<String>,
}

// ── Dispatch ──────────────────────────────────────────────────────────────────

/// Look up a command in the registry and run it.
/// Returns the display string on success.
pub async fn dispatch(registry: &Registry, command: &str, args: &[String]) -> Result<String> {
    let resolved = registry.commands.get(command).with_context(|| {
        format!(
            "unknown command '{}'. Run `hm help` to see available commands.",
            command
        )
    })?;

    run(registry, resolved, args).await
}

/// Run a resolved command against its plugin.
pub async fn run(
    registry: &Registry,
    resolved: &ResolvedCommand,
    args: &[String],
) -> Result<String> {
    // Merge: global env < plugin manifest env
    // (we intentionally do NOT forward process env — plugins get only what's declared)
    let mut env = registry.config.env.clone();
    env.extend(resolved.plugin.manifest.env.clone());

    let request = PluginRequest {
        command: resolved.meta.name.clone(),
        args: args.to_vec(),
        env,
    };

    let request_json = serde_json::to_string(&request)?;

    // Resolve the runtime binary
    let runtime_bin = resolve_runtime(&resolved.plugin.manifest.run.runtime)?;

    // Spawn: <runtime> <entry_point>  (entry is an absolute path)
    let mut child = Command::new(&runtime_bin)
        .arg(&resolved.plugin.entry)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        // Run with the plugin's directory as cwd
        .current_dir(&resolved.plugin.dir)
        .spawn()
        .with_context(|| {
            format!(
                "spawning {} for plugin '{}'",
                runtime_bin,
                resolved.plugin.manifest.plugin.name
            )
        })?;

    // Write JSON request to stdin, explicitly flush and drop to close the pipe.
    // On Windows, Node's stdin "end" event only fires once the write end is closed.
    // Simply dropping the handle inside an if-let is not sufficient with tokio on
    // Windows — we must take it, write, flush, then drop before waiting.
    {
        let mut stdin = child.stdin.take().expect("stdin was piped");
        stdin.write_all(request_json.as_bytes()).await?;
        stdin.flush().await?;
        drop(stdin);
    }

    // wait_with_output() deadlocks on Windows when stdout fills the pipe buffer.
    // Fix: read stdout/stderr and wait for exit concurrently.
    let mut stdout_handle = child.stdout.take().expect("stdout was piped");
    let mut stderr_handle = child.stderr.take().expect("stderr was piped");

    let (stdout_bytes, stderr_bytes, status) = tokio::join!(
        async {
            let mut buf = Vec::new();
            tokio::io::AsyncReadExt::read_to_end(&mut stdout_handle, &mut buf).await?;
            Ok::<Vec<u8>, std::io::Error>(buf)
        },
        async {
            let mut buf = Vec::new();
            tokio::io::AsyncReadExt::read_to_end(&mut stderr_handle, &mut buf).await?;
            Ok::<Vec<u8>, std::io::Error>(buf)
        },
        child.wait()
    );

    let status   = status?;
    let stdout_bytes = stdout_bytes?;
    let stderr_bytes = stderr_bytes?;

    if !status.success() {
        let stderr = String::from_utf8_lossy(&stderr_bytes);
        bail!(
            "plugin '{}' exited with error:\n{}",
            resolved.plugin.manifest.plugin.name,
            stderr.trim()
        );
    }

    let stdout = String::from_utf8_lossy(&stdout_bytes);
    if stdout.trim().is_empty() {
        bail!(
            "plugin '{}' produced no output",
            resolved.plugin.manifest.plugin.name
        );
    }

    let response: PluginResponse = serde_json::from_str(stdout.trim()).with_context(|| {
        format!(
            "parsing response from plugin '{}': got: {}",
            resolved.plugin.manifest.plugin.name,
            stdout.trim()
        )
    })?;

    if response.status == "error" {
        bail!(
            "{}",
            response.error.unwrap_or_else(|| "unknown error".into())
        );
    }

    Ok(response.output.unwrap_or_default())
}

/// Map a runtime name to its executable.
fn resolve_runtime(runtime: &str) -> Result<String> {
    match runtime {
        "node" => Ok("node".into()),
        "python" | "python3" => Ok("python3".into()),
        "bash" | "sh" => Ok("bash".into()),
        "binary" => Ok(String::new()), // entry IS the binary — handled separately
        other => bail!(
            "unknown runtime '{}'. Supported: node, python, bash, binary",
            other
        ),
    }
}