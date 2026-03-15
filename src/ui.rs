//! Rendering — pure functions from App state → ratatui widgets.
//! No mutation here, no event handling.

use crate::app::{App, LineKind};
use ratatui::style::Stylize;
use ratatui::{
    layout::{Alignment, Constraint, Direction, Layout, Rect},
    style::{Color, Modifier, Style},
    text::{Line, Span},
    widgets::{Block, Borders, List, ListItem, Paragraph, Tabs, Wrap},
    Frame,
};

// ── Palette ───────────────────────────────────────────────────────────────────

const BG: Color       = Color::Rgb(8, 10, 8);
const PANEL: Color    = Color::Rgb(13, 17, 13);
const GREEN: Color    = Color::Rgb(0, 255, 100);
const DIM: Color      = Color::Rgb(0, 110, 50);
const ACCENT: Color   = Color::Rgb(0, 220, 180);
const ERR: Color      = Color::Rgb(255, 80, 70);
const TEXT: Color     = Color::Rgb(190, 220, 200);

// ── Entry point ───────────────────────────────────────────────────────────────

pub fn render(f: &mut Frame, app: &App) {
    // Fill background
    f.render_widget(Block::default().style(Style::default().bg(BG)), f.size());

    let root = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Length(3), // tabs / header
            Constraint::Min(0),    // body
            Constraint::Length(3), // input bar
        ])
        .split(f.size());

    render_tabs(f, app, root[0]);
    render_body(f, app, root[1]);
    render_input(f, app, root[2]);
}

// ── Tabs ──────────────────────────────────────────────────────────────────────

fn render_tabs(f: &mut Frame, app: &App, area: Rect) {
    let labels: Vec<Line> = app
        .tab_labels()
        .into_iter()
        .map(|t| Line::from(Span::styled(format!(" {} ", t), Style::default().fg(DIM))))
        .collect();

    let tabs = Tabs::new(labels)
        .select(app.active_tab)
        .block(
            Block::default()
                .borders(Borders::ALL)
                .border_style(Style::default().fg(DIM))
                .title(Span::styled(
                    " ⬡ hm ",
                    Style::default().fg(GREEN).add_modifier(Modifier::BOLD),
                ))
                .title_alignment(Alignment::Left)
                .bg(PANEL),
        )
        .highlight_style(
            Style::default()
                .fg(ACCENT)
                .add_modifier(Modifier::BOLD),
        )
        .divider(Span::styled("│", Style::default().fg(DIM)));

    f.render_widget(tabs, area);
}

// ── Body ──────────────────────────────────────────────────────────────────────

fn render_body(f: &mut Frame, app: &App, area: Rect) {
    let chunks = Layout::default()
        .direction(Direction::Horizontal)
        .constraints([Constraint::Min(0), Constraint::Length(30)])
        .split(area);

    render_output(f, app, chunks[0]);
    render_sidebar(f, app, chunks[1]);
}

fn render_output(f: &mut Frame, app: &App, area: Rect) {
    let inner_height = area.height.saturating_sub(2) as usize;
    let total = app.output.len();

    // Apply scroll: scroll=0 means bottom-aligned
    let end = total.saturating_sub(app.scroll);
    let start = end.saturating_sub(inner_height);
    let visible = &app.output[start..end];

    let items: Vec<ListItem> = visible
        .iter()
        .map(|line| {
            let style = match line.kind {
                LineKind::Normal   => Style::default().fg(TEXT),
                LineKind::Success  => Style::default().fg(GREEN),
                LineKind::Error    => Style::default().fg(ERR),
                LineKind::Dim      => Style::default().fg(DIM),
                LineKind::Command  => Style::default().fg(ACCENT).add_modifier(Modifier::BOLD),
            };
            ListItem::new(Line::from(Span::styled(line.text.clone(), style)))
        })
        .collect();

    let tab_labels = app.tab_labels();
    let title = tab_labels
        .get(app.active_tab)
        .map(String::as_str)
        .unwrap_or("all");

    let list = List::new(items).block(
        Block::default()
            .borders(Borders::ALL)
            .border_style(Style::default().fg(DIM))
            .title(Span::styled(
                format!(" {} ", title),
                Style::default().fg(ACCENT),
            ))
            .bg(BG),
    );

    f.render_widget(list, area);
}

fn render_sidebar(f: &mut Frame, app: &App, area: Rect) {
    let mut cmds = app.visible_commands();
    cmds.sort_by_key(|(name, _)| *name);

    let mut lines: Vec<Line> = if cmds.is_empty() {
        vec![Line::from(Span::styled("no commands", Style::default().fg(DIM)))]
    } else {
        cmds.iter()
            .map(|(name, desc)| {
                Line::from(vec![
                    Span::styled(format!(" {:12}", name), Style::default().fg(ACCENT)),
                    Span::styled(
                        // truncate description to fit sidebar width
                        truncate(desc, 16),
                        Style::default().fg(DIM),
                    ),
                ])
            })
            .collect()
    };

    // Dev plugin indicators
    let dev_plugins: Vec<&str> = app
        .registry
        .plugins
        .iter()
        .filter(|p| p.is_dev)
        .map(|p| p.manifest.plugin.name.as_str())
        .collect();
    if !dev_plugins.is_empty() {
        lines.push(Line::from(""));
        lines.push(Line::from(Span::styled("─── dev ────────", Style::default().fg(DIM))));
        for name in dev_plugins {
            lines.push(Line::from(Span::styled(
                format!(" * {}", name),
                Style::default().fg(Color::Rgb(255, 200, 50)),
            )));
        }
    }

    // Key hint footer
    lines.push(Line::from(""));
    lines.push(Line::from(Span::styled("─── keys ───────", Style::default().fg(DIM))));
    for (key, action) in [
        ("Tab / S-Tab", "switch tab"),
        ("Enter", "run command"),
        ("↑ / ↓", "scroll"),
        ("Ctrl-R", "reload plugins"),
        ("Ctrl-C", "quit"),
    ] {
        lines.push(Line::from(vec![
            Span::styled(format!(" {:12}", key), Style::default().fg(Color::Rgb(100, 130, 110))),
            Span::styled(action, Style::default().fg(DIM)),
        ]));
    }

    let sidebar = Paragraph::new(lines)
        .block(
            Block::default()
                .borders(Borders::ALL)
                .border_style(Style::default().fg(DIM))
                .title(Span::styled(" commands ", Style::default().fg(GREEN)))
                .bg(PANEL),
        )
        .wrap(Wrap { trim: true });

    f.render_widget(sidebar, area);
}

// ── Input bar ─────────────────────────────────────────────────────────────────

fn render_input(f: &mut Frame, app: &App, area: Rect) {
    let line = Line::from(vec![
        Span::styled(" › ", Style::default().fg(GREEN).add_modifier(Modifier::BOLD)),
        Span::styled(app.input.clone(), Style::default().fg(Color::White)),
        Span::styled("▌", Style::default().fg(GREEN)),
    ]);

    let input = Paragraph::new(line).block(
        Block::default()
            .borders(Borders::ALL)
            .border_style(Style::default().fg(DIM))
            .bg(PANEL),
    );

    f.render_widget(input, area);
}

// ── Helpers ───────────────────────────────────────────────────────────────────

fn truncate(s: &str, max: usize) -> String {
    if s.len() <= max {
        s.to_string()
    } else {
        format!("{}…", &s[..max.saturating_sub(1)])
    }
}