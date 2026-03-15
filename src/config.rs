//! Config loading and plugin discovery.
//!
//! On startup, we:
//!   1. Load ~/.config/hackermode/config.toml  (global env + settings)
//!   2. Scan ~/.config/hackermode/plugins/      (one subdir per plugin)
//!   3. Load any [dev] plugin paths from config (last, so they override installed)
//!   4. Build a flat Registry: command name → (plugin, manifest entry)
//!      Last plugin loaded wins on name conflicts.

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::path::{Path, PathBuf};

// ── Global config ─────────────────────────────────────────────────────────────

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct GlobalConfig {
    /// Key-value env vars passed to every plugin invocation
    #[serde(default)]
    pub env: HashMap<String, String>,
    /// Dev overrides — local plugin paths loaded after the normal scan
    #[serde(default)]
    pub dev: DevConfig,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct DevConfig {
    /// Absolute paths to local plugin folders.
    /// Each path must contain a manifest.toml.
    /// Loaded last so they override any installed plugin with the same name.
    #[serde(default)]
    pub plugins: Vec<PathBuf>,
}

impl GlobalConfig {
    fn load(path: &Path) -> Result<Self> {
        if !path.exists() {
            return Ok(Self::default());
        }
        let raw = std::fs::read_to_string(path)
            .with_context(|| format!("reading {}", path.display()))?;
        toml::from_str(&raw).with_context(|| "parsing config.toml")
    }
}

// ── manifest.toml ─────────────────────────────────────────────────────────────

#[derive(Debug, Clone, Deserialize)]
pub struct Manifest {
    pub plugin: PluginMeta,
    pub run: RunConfig,
    #[serde(default)]
    pub commands: Vec<CommandMeta>,
    /// Plugin-specific env vars (e.g. GITHUB_TOKEN)
    #[serde(default)]
    pub env: HashMap<String, String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct PluginMeta {
    pub name: String,
    pub description: String,
    #[serde(default = "default_version")]
    pub version: String,
}

fn default_version() -> String {
    "0.1.0".into()
}

#[derive(Debug, Clone, Deserialize)]
pub struct RunConfig {
    /// Runtime to use: "node", "python", "bash", or "binary"
    pub runtime: String,
    /// Entry point, relative to the plugin directory
    pub entry: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CommandMeta {
    pub name: String,
    pub description: String,
    #[serde(default)]
    pub args: Vec<ArgMeta>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ArgMeta {
    pub name: String,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub required: bool,
}

// ── Loaded plugin (manifest + resolved paths) ─────────────────────────────────

#[derive(Debug, Clone)]
pub struct LoadedPlugin {
    pub manifest: Manifest,
    /// Absolute path to the plugin directory
    pub dir: PathBuf,
    /// Absolute path to the entry point
    pub entry: PathBuf,
    /// True if this plugin was loaded from [dev] paths
    pub is_dev: bool,
}

impl LoadedPlugin {
    fn load(dir: PathBuf, is_dev: bool) -> Result<Self> {
        let manifest_path = dir.join("manifest.toml");
        let raw = std::fs::read_to_string(&manifest_path)
            .with_context(|| format!("reading {}", manifest_path.display()))?;
        let manifest: Manifest =
            toml::from_str(&raw).with_context(|| format!("parsing {}", manifest_path.display()))?;

        let entry = dir.join(&manifest.run.entry);
        anyhow::ensure!(
            entry.exists(),
            "plugin '{}' entry point not found: {}",
            manifest.plugin.name,
            entry.display()
        );

        Ok(Self { manifest, dir, entry, is_dev })
    }
}

// ── Registry ──────────────────────────────────────────────────────────────────

/// A resolved command ready to dispatch
#[derive(Debug, Clone)]
pub struct ResolvedCommand {
    pub meta: CommandMeta,
    pub plugin: LoadedPlugin,
}

/// The flat command registry built at startup.
/// Last plugin loaded wins on name conflicts.
#[derive(Debug, Clone)]
pub struct Registry {
    pub config: GlobalConfig,
    /// All successfully loaded plugins, in load order
    pub plugins: Vec<LoadedPlugin>,
    /// Flat map: command name → resolved command
    pub commands: HashMap<String, ResolvedCommand>,
    /// Plugins/paths that failed to load: (label, error message)
    pub errors: Vec<(String, String)>,
}

impl Registry {
    /// Load config + scan plugins directory + apply dev overrides.
    pub fn load() -> Result<Self> {
        let config_dir = Self::config_dir();
        let plugins_dir = config_dir.join("plugins");

        // Ensure directories exist
        std::fs::create_dir_all(&plugins_dir)
            .with_context(|| format!("creating {}", plugins_dir.display()))?;

        // Load global config (includes [dev] section)
        let config = GlobalConfig::load(&config_dir.join("config.toml"))?;

        let mut plugins: Vec<LoadedPlugin> = Vec::new();
        let mut commands: HashMap<String, ResolvedCommand> = HashMap::new();
        let mut errors: Vec<(String, String)> = Vec::new();

        // ── 1. Scan installed plugins (alphabetical) ──────────────────────────
        let mut plugin_dirs: Vec<PathBuf> = std::fs::read_dir(&plugins_dir)
            .with_context(|| format!("scanning {}", plugins_dir.display()))?
            .filter_map(|e| e.ok())
            .map(|e| e.path())
            .filter(|p| p.is_dir())
            .collect();
        plugin_dirs.sort();

        for dir in plugin_dirs {
            load_one(dir, false, &mut plugins, &mut commands, &mut errors);
        }

        // ── 2. Load dev paths (override installed — last write wins) ──────────
        for dev_path in &config.dev.plugins {
            let abs = if dev_path.is_absolute() {
                dev_path.clone()
            } else {
                // Resolve relative paths from config dir
                config_dir.join(dev_path)
            };

            if !abs.exists() {
                errors.push((
                    abs.display().to_string(),
                    "dev path does not exist".into(),
                ));
                continue;
            }

            load_one(abs, true, &mut plugins, &mut commands, &mut errors);
        }

        Ok(Self { config, plugins, commands, errors })
    }

    pub fn config_dir() -> PathBuf {
        dirs::config_dir()
            .unwrap_or_else(|| PathBuf::from("."))
            .join("hackermode")
    }
}

/// Load a single plugin folder, registering its commands.
/// Extracted so both the scan loop and dev loop share the same logic.
fn load_one(
    dir: PathBuf,
    is_dev: bool,
    plugins: &mut Vec<LoadedPlugin>,
    commands: &mut HashMap<String, ResolvedCommand>,
    errors: &mut Vec<(String, String)>,
) {
    let label = dir.display().to_string();
    match LoadedPlugin::load(dir, is_dev) {
        Ok(plugin) => {
            // If a dev plugin overrides an existing one, remove the old entry
            // from the plugins list so the TUI doesn't show it twice
            let name = plugin.manifest.plugin.name.clone();
            plugins.retain(|p| p.manifest.plugin.name != name);

            // Register commands — last write wins
            for cmd in &plugin.manifest.commands {
                commands.insert(
                    cmd.name.clone(),
                    ResolvedCommand {
                        meta: cmd.clone(),
                        plugin: plugin.clone(),
                    },
                );
            }
            plugins.push(plugin);
        }
        Err(e) => {
            errors.push((label, format!("{:#}", e)));
        }
    }
}