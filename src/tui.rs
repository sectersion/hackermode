//! TUI event loop — terminal setup, input handling, async command dispatch.

use crate::app::{App, LineKind};
use crate::config::Registry;
use crate::plugin;
use crate::ui;
use anyhow::Result;
use crossterm::{
    event::{self, DisableMouseCapture, EnableMouseCapture, Event, KeyCode, KeyModifiers},
    execute,
    terminal::{disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen},
};
use ratatui::{backend::CrosstermBackend, Terminal};
use std::{io, time::Duration};

pub async fn run(registry: Registry) -> Result<()> {
    // ── Terminal setup ────────────────────────────────────────────────────────
    enable_raw_mode()?;
    let mut stdout = io::stdout();
    execute!(stdout, EnterAlternateScreen, EnableMouseCapture)?;
    let backend = CrosstermBackend::new(stdout);
    let mut terminal = Terminal::new(backend)?;

    let mut app = App::new(registry);
    let result = event_loop(&mut terminal, &mut app).await;

    // ── Restore terminal (always, even on error) ──────────────────────────────
    disable_raw_mode()?;
    execute!(
        terminal.backend_mut(),
        LeaveAlternateScreen,
        DisableMouseCapture
    )?;
    terminal.show_cursor()?;

    result
}

async fn event_loop<B: ratatui::backend::Backend>(
    terminal: &mut Terminal<B>,
    app: &mut App,
) -> Result<()> {
    loop {
        terminal.draw(|f| ui::render(f, app))?;

        // Non-blocking poll — 50 ms tick keeps the TUI responsive
        if !event::poll(Duration::from_millis(50))? {
            continue;
        }

        if let Event::Key(key) = event::read()? {
            if key.kind == event::KeyEventKind::Press {
                handle_key(app, key.modifiers, key.code).await;
            }
        }

        if app.should_quit {
            break;
        }
    }
    Ok(())
}

async fn handle_key(app: &mut App, modifiers: KeyModifiers, code: KeyCode) {
    match (modifiers, code) {
        // ── Quit ──────────────────────────────────────────────────────────────
        (KeyModifiers::CONTROL, KeyCode::Char('c'))
        | (KeyModifiers::CONTROL, KeyCode::Char('d')) => {
            app.should_quit = true;
        }

        // ── Reload registry (dev workflow) ────────────────────────────────────
        (KeyModifiers::CONTROL, KeyCode::Char('r')) => {
            app.push("reloading plugins...", LineKind::Dim);
            match Registry::load() {
                Ok(registry) => {
                    let dev_count = registry.plugins.iter().filter(|p| p.is_dev).count();
                    let total = registry.plugins.len();
                    app.reload(registry);
                    app.push(
                        format!(
                            "reloaded — {} plugin(s) loaded ({} dev)",
                            total, dev_count
                        ),
                        LineKind::Success,
                    );
                }
                Err(e) => {
                    app.push(format!("reload failed: {}", e), LineKind::Error);
                }
            }
        }

        // ── Tab navigation ────────────────────────────────────────────────────
        (KeyModifiers::NONE, KeyCode::Tab) => app.next_tab(),
        (KeyModifiers::SHIFT, KeyCode::BackTab) => app.prev_tab(),

        // ── Scroll ────────────────────────────────────────────────────────────
        (KeyModifiers::NONE, KeyCode::Up) => app.scroll_up(),
        (KeyModifiers::NONE, KeyCode::Down) => app.scroll_down(),

        // ── Input: typing ─────────────────────────────────────────────────────
        (KeyModifiers::NONE | KeyModifiers::SHIFT, KeyCode::Char(c)) => {
            app.input.push(c);
        }
        (KeyModifiers::NONE, KeyCode::Backspace) => {
            app.input.pop();
        }
        (KeyModifiers::NONE, KeyCode::Delete) => {
            app.input.clear();
        }

        // ── Input: execute ────────────────────────────────────────────────────
        (KeyModifiers::NONE, KeyCode::Enter) => {
            let raw = app.input.trim().to_string();
            if raw.is_empty() {
                return;
            }
            app.input.clear();

            // Echo the command
            app.push(format!(" › {}", raw), LineKind::Command);

            // Parse: first token = command, rest = args
            let mut parts = raw.splitn(64, ' ').map(String::from);
            let command = match parts.next() {
                Some(c) if !c.is_empty() => c,
                _ => return,
            };
            let args: Vec<String> = parts.filter(|s| !s.is_empty()).collect();

            // ── Built-in commands (handled in core, not plugins) ─────────
            match command.as_str() {
                "exit" | "quit" => {
                    app.should_quit = true;
                    return;
                }
                _ => {}
            }

            // Dispatch to plugin
            let registry = app.registry.clone();
            match plugin::dispatch(&registry, &command, &args).await {
                Ok(output) => {
                    if output.is_empty() as bool {
                        app.push("(no output)", LineKind::Dim);
                    } else {
                        for line in output.lines() {
                            app.push(line, LineKind::Normal);
                        }
                        app.push("", LineKind::Dim); // spacer
                    }
                }
                Err(e) => {
                    app.push(format!("error: {}", e), LineKind::Error);
                }
            }
        }

        _ => {}
    }
}