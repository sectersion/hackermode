//! Central TUI application state.

use crate::config::Registry;

#[derive(Debug, Clone, PartialEq)]
pub enum LineKind {
    Normal,
    Success,
    Error,
    Dim,
    Command,
}

#[derive(Debug, Clone)]
pub struct OutputLine {
    pub text: String,
    pub kind: LineKind,
}

impl OutputLine {
    pub fn new(text: impl Into<String>, kind: LineKind) -> Self {
        Self { text: text.into(), kind }
    }
}

#[derive(Debug)]
pub struct App {
    pub registry: Registry,

    /// Index into registry.plugins for the active tab (0 = "all")
    pub active_tab: usize,

    /// Output buffer shown in the main panel
    pub output: Vec<OutputLine>,

    /// Current text in the command input bar
    pub input: String,

    /// How many lines we've scrolled up from the bottom
    pub scroll: usize,

    /// Set to true to exit the event loop
    pub should_quit: bool,
}

impl App {
    pub fn new(registry: Registry) -> Self {
        let mut app = Self {
            registry,
            active_tab: 0,
            output: Vec::new(),
            input: String::new(),
            scroll: 0,
            should_quit: false,
        };

        app.push(
            "hackermode — type a command or `help`",
            LineKind::Dim,
        );

        if !app.registry.errors.is_empty() {
            for (name, err) in &app.registry.errors.clone() {
                app.push(format!("warn: plugin '{}' failed to load: {}", name, err), LineKind::Error);
            }
        }

        app
    }

    pub fn push(&mut self, text: impl Into<String>, kind: LineKind) {
        self.output.push(OutputLine::new(text, kind));
        // Reset scroll to bottom on new output
        self.scroll = 0;
    }

    /// The list of tab labels: ["all", plugin1, plugin2, ...]
    pub fn tab_labels(&self) -> Vec<String> {
        let mut labels = vec!["all".to_string()];
        for p in &self.registry.plugins {
            labels.push(p.manifest.plugin.name.clone());
        }
        labels
    }

    /// Commands visible in the current tab (filtered by plugin if not "all")
    pub fn visible_commands(&self) -> Vec<(&str, &str)> {
        let labels = self.tab_labels();
        let active = labels.get(self.active_tab).map(String::as_str).unwrap_or("all");

        self.registry
            .commands
            .values()
            .filter(|rc| {
                active == "all" || rc.plugin.manifest.plugin.name == active
            })
            .map(|rc| (rc.meta.name.as_str(), rc.meta.description.as_str()))
            .collect()
    }

    pub fn next_tab(&mut self) {
        let count = self.registry.plugins.len() + 1; // +1 for "all"
        self.active_tab = (self.active_tab + 1) % count;
    }

    pub fn prev_tab(&mut self) {
        let count = self.registry.plugins.len() + 1;
        if self.active_tab == 0 {
            self.active_tab = count - 1;
        } else {
            self.active_tab -= 1;
        }
    }

    pub fn scroll_up(&mut self) {
        self.scroll = self.scroll.saturating_add(1);
    }

    pub fn scroll_down(&mut self) {
        self.scroll = self.scroll.saturating_sub(1);
    }
}

impl App {
    /// Replace the registry with a freshly loaded one, preserving output history.
    pub fn reload(&mut self, registry: Registry) {
        // Clamp active tab to valid range
        let max_tab = registry.plugins.len();
        if self.active_tab > max_tab {
            self.active_tab = 0;
        }
        self.registry = registry;
    }
}