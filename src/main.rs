mod app;
mod config;
mod plugin;
mod tui;
mod ui;

use anyhow::Result;
use clap::Parser;

#[derive(Parser, Debug)]
#[command(
    name = "hm",
    about = "hackermode — a universal CLI layer",
    version = "0.1.0",
    long_about = "Drop into hacker mode. Run `hm` for the TUI, or `hm <command> [args]` headless."
)]
struct Cli {
    /// Command to run (e.g. help, prs, ask). Omit to open the TUI.
    command: Option<String>,

    /// Arguments passed to the command
    #[arg(trailing_var_arg = true, allow_hyphen_values = true)]
    args: Vec<String>,
}

#[tokio::main]
async fn main() -> Result<()> {
    let cli = Cli::parse();

    // Load config + scan plugins dir, building the flat command registry
    let registry = config::Registry::load()?;

    match cli.command {
        // No command → open TUI
        None => {
            tui::run(registry).await?;
        }
        // hm <command> [args...] → headless dispatch
        Some(command) => {
            let output = plugin::dispatch(&registry, &command, &cli.args).await?;
            println!("{}", output);
        }
    }

    Ok(())
}