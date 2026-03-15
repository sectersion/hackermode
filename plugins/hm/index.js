/**
 * hm — hackermode core plugin
 *
 * Commands:
 *   help                          list all commands across all plugins
 *   version                       print hackermode version
 *   plugins                       list installed plugins with status
 *   config                        print current config file
 *   install                       install hm binary to PATH
 *   uninstall                     remove hm binary from PATH
 *   plugin list                   browse the registry
 *   plugin info <name>            show registry details for a plugin
 *   plugin add <name>             install a plugin from the registry
 *   plugin remove <name>          uninstall a plugin
 *   plugin enable <name>          re-enable a disabled plugin
 *   plugin disable <name>         disable a plugin without removing it
 *   plugin installed              list locally installed plugins
 */

"use strict";

const fs            = require("fs");
const path          = require("path");
const os            = require("os");
const { execSync }  = require("child_process");

// ── Constants ─────────────────────────────────────────────────────────────────

const VERSION       = "0.1.0";
const REGISTRY_BASE = "https://raw.githubusercontent.com/hackermode/plugins/main";
const REGISTRY_JSON = `${REGISTRY_BASE}/registry.json`;

// ── Platform paths ────────────────────────────────────────────────────────────

function configDir() {
  if (process.platform === "win32") {
    return path.join(process.env.APPDATA || os.homedir(), "hackermode");
  }
  if (process.platform === "darwin") {
    return path.join(os.homedir(), "Library", "Application Support", "hackermode");
  }
  return path.join(os.homedir(), ".config", "hackermode");
}

function pluginsDir()       { return path.join(configDir(), "plugins"); }
function pluginDir(name)    { return path.join(pluginsDir(), name); }
function configFile()       { return path.join(configDir(), "config.toml"); }
function binaryDest()       {
  const ext = process.platform === "win32" ? ".exe" : "";
  return path.join(configDir(), `hm${ext}`);
}

// ── HTTP helpers (zero deps — Node built-in https) ────────────────────────────

async function fetchText(url) {
  const mod = url.startsWith("https") ? require("https") : require("http");
  return new Promise((resolve, reject) => {
    mod.get(url, (res) => {
      if (res.statusCode === 301 || res.statusCode === 302) {
        return fetchText(res.headers.location).then(resolve).catch(reject);
      }
      if (res.statusCode !== 200) {
        return reject(new Error(`HTTP ${res.statusCode}: ${url}`));
      }
      let data = "";
      res.on("data", (c) => (data += c));
      res.on("end", () => resolve(data));
    }).on("error", reject);
  });
}

async function fetchFile(url, dest) {
  const mod = url.startsWith("https") ? require("https") : require("http");
  return new Promise((resolve, reject) => {
    mod.get(url, (res) => {
      if (res.statusCode === 301 || res.statusCode === 302) {
        return fetchFile(res.headers.location, dest).then(resolve).catch(reject);
      }
      if (res.statusCode !== 200) {
        return reject(new Error(`HTTP ${res.statusCode}: ${url}`));
      }
      const file = fs.createWriteStream(dest);
      res.pipe(file);
      file.on("finish", () => file.close(resolve));
      file.on("error", reject);
    }).on("error", reject);
  });
}

// ── Minimal TOML parser (handles subset used in manifests + install.toml) ─────

function parseToml(text) {
  const result = {};
  let section = result;

  for (const rawLine of text.split("\n")) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#")) continue;

    const secMatch = line.match(/^\[([^\]]+)\]$/);
    if (secMatch) {
      const key = secMatch[1].trim();
      result[key] = result[key] || {};
      section = result[key];
      continue;
    }

    const eqIdx = line.indexOf("=");
    if (eqIdx === -1) continue;

    const key = line.slice(0, eqIdx).trim();
    const raw = line.slice(eqIdx + 1).trim();

    if (raw.startsWith('"') || raw.startsWith("'")) {
      section[key] = raw.slice(1, -1);
    } else if (raw.startsWith("[")) {
      section[key] = raw
        .slice(1, raw.lastIndexOf("]"))
        .split(",")
        .map((s) => s.trim().replace(/^["']|["']$/g, ""))
        .filter(Boolean);
    } else {
      section[key] = raw;
    }
  }
  return result;
}

// ── Registry ──────────────────────────────────────────────────────────────────

async function fetchRegistry() {
  return JSON.parse(await fetchText(REGISTRY_JSON));
}

async function fetchInstallToml(name) {
  return parseToml(await fetchText(`${REGISTRY_BASE}/${name}/install.toml`));
}

async function fetchPluginFileList(name) {
  try {
    const json = await fetchText(
      `https://api.github.com/repos/hackermode/plugins/contents/${name}`
    );
    return JSON.parse(json)
      .filter((e) => e.type === "file")
      .map((e) => e.name);
  } catch {
    // Fallback if GitHub API rate-limits us
    return ["manifest.toml", "index.js"];
  }
}

// ── Plugin installer ──────────────────────────────────────────────────────────

async function installPlugin(name, visited = new Set(), lines = []) {
  if (visited.has(name)) return lines;
  visited.add(name);

  lines.push(`→ installing ${name}...`);

  // Read install.toml (optional — no error if absent)
  let installToml = {};
  try {
    installToml = await fetchInstallToml(name);
  } catch { /* no install.toml, that's fine */ }

  const deps    = installToml?.install?.plugins || [];
  const runCmds = installToml?.install?.run     || [];

  // 1. Install dependencies first (recursive)
  for (const dep of deps) {
    if (fs.existsSync(pluginDir(dep))) {
      lines.push(`  ✓ dependency '${dep}' already installed`);
    } else {
      await installPlugin(dep, visited, lines);
    }
  }

  // 2. Download all files from registry folder
  const dest = pluginDir(name);
  fs.mkdirSync(dest, { recursive: true });

  const files = await fetchPluginFileList(name);
  for (const file of files) {
    if (file === "install.toml") continue;
    lines.push(`  ↓ ${file}`);
    await fetchFile(`${REGISTRY_BASE}/${name}/${file}`, path.join(dest, file));
  }

  // 3. Run install commands in plugin folder
  for (const cmd of runCmds) {
    lines.push(`  $ ${cmd}`);
    try {
      execSync(cmd, { cwd: dest, stdio: "pipe" });
    } catch (e) {
      throw new Error(`install command failed: ${cmd}\n${e.message}`);
    }
  }

  lines.push(`  ✓ ${name} installed`);
  return lines;
}

// ── Local plugin helpers ──────────────────────────────────────────────────────

function loadLocalManifest(name) {
  const p = path.join(pluginDir(name), "manifest.toml");
  if (!fs.existsSync(p)) return null;
  return parseToml(fs.readFileSync(p, "utf8"));
}

function listInstalledPlugins() {
  if (!fs.existsSync(pluginsDir())) return [];
  return fs.readdirSync(pluginsDir())
    .filter((e) => fs.statSync(path.join(pluginsDir(), e)).isDirectory());
}

function listDisabledPlugins() {
  if (!fs.existsSync(pluginsDir())) return [];
  return fs.readdirSync(pluginsDir())
    .filter((e) => e.endsWith(".disabled"))
    .map((e) => e.replace(".disabled", ""));
}

// ── PATH helpers ──────────────────────────────────────────────────────────────

function isOnPath(dir) {
  const pathDirs = (process.env.PATH || "").split(path.delimiter);
  return pathDirs.some((p) => p.toLowerCase() === dir.toLowerCase());
}

function addToPathWindows(dir) {
  // Permanently add dir to user PATH via setx
  const currentPath = execSync("powershell -Command \"[Environment]::GetEnvironmentVariable('PATH','User')\"", {
    encoding: "utf8",
  }).trim();
  if (currentPath.toLowerCase().includes(dir.toLowerCase())) return false;
  execSync(`setx PATH "${currentPath};${dir}"`, { stdio: "pipe" });
  return true;
}

function removeFromPathWindows(dir) {
  const currentPath = execSync("powershell -Command \"[Environment]::GetEnvironmentVariable('PATH','User')\"", {
    encoding: "utf8",
  }).trim();
  const parts = currentPath.split(";").filter((p) => p.toLowerCase() !== dir.toLowerCase());
  execSync(`setx PATH "${parts.join(";")}"`, { stdio: "pipe" });
}

// ── Command handlers ──────────────────────────────────────────────────────────

async function cmdHelp() {
  const installed = listInstalledPlugins();
  const lines = [
    "hackermode — universal CLI layer",
    "",
    "usage:  hm <command> [args]",
    "        hm                   open the TUI",
    "",
    "built-in commands:",
    "  exit / quit          exit hackermode",
    "",
  ];

  if (installed.length === 0) {
    lines.push("no plugins installed.");
    lines.push("run `plugin add <name>` to install one.");
  } else {
    lines.push("plugin commands:");
    for (const name of installed) {
      const manifest = loadLocalManifest(name);
      if (!manifest) continue;
      const cmds = (manifest.commands || []);
      lines.push(`  ── ${name} — ${manifest.plugin?.description || ""}`);
      for (const cmd of cmds) {
        const cname = cmd.name || cmd;
        const cdesc = cmd.description || "";
        lines.push(`     ${cname.padEnd(16)} ${cdesc}`);
      }
    }
  }

  return { status: "success", output: lines.join("\n") };
}

async function cmdVersion() {
  return { status: "success", output: `hackermode v${VERSION}` };
}

async function cmdPlugins() {
  const installed = listInstalledPlugins();
  const disabled  = listDisabledPlugins();

  if (installed.length === 0 && disabled.length === 0) {
    return {
      status: "success",
      output: "no plugins installed.\n\nrun `plugin list` to browse the registry.",
    };
  }

  const lines = ["installed plugins:", ""];
  for (const name of installed) {
    const m = loadLocalManifest(name);
    lines.push(`  ✓  ${name.padEnd(16)} v${m?.plugin?.version || "?"}  ${m?.plugin?.description || ""}`);
  }
  for (const name of disabled) {
    lines.push(`  ✗  ${name.padEnd(16)} (disabled)`);
  }
  return { status: "success", output: lines.join("\n") };
}

async function cmdConfig() {
  const cfgPath = configFile();
  if (!fs.existsSync(cfgPath)) {
    return {
      status: "success",
      output: [
        `config not found at: ${cfgPath}`,
        "",
        "create it to configure hackermode:",
        "",
        "[env]",
        "# global env vars passed to every plugin",
        "# GITHUB_TOKEN = \"ghp_...\"",
        "",
        "[dev]",
        "# local plugin paths for development",
        `# plugins = ["C:\\\\path\\\\to\\\\my-plugin"]`,
      ].join("\n"),
    };
  }
  return {
    status: "success",
    output: `# ${cfgPath}\n\n${fs.readFileSync(cfgPath, "utf8")}`,
  };
}

async function cmdInstall() {
  const dest    = binaryDest();
  const destDir = configDir();
  const lines   = [];

  // Find the built binary next to this plugin's directory
  // Walk up from plugins/hm → config dir → look for hm.exe in common build locations
  const candidates = [
    // Running from cargo dev build
    path.join(__dirname, "..", "..", "target", "release", process.platform === "win32" ? "hm.exe" : "hm"),
    path.join(__dirname, "..", "..", "target", "debug",   process.platform === "win32" ? "hm.exe" : "hm"),
  ];

  const src = candidates.find((p) => fs.existsSync(p));
  if (!src) {
    return {
      status: "error",
      error: [
        "could not find hm binary. build it first:",
        "",
        "  cargo build --release",
        "",
        "then run `install` again.",
      ].join("\n"),
    };
  }

  // Copy binary
  fs.mkdirSync(destDir, { recursive: true });
  fs.copyFileSync(src, dest);
  lines.push(`✓ copied binary to ${dest}`);

  // Add to PATH
  if (process.platform === "win32") {
    try {
      const added = addToPathWindows(destDir);
      if (added) {
        lines.push(`✓ added ${destDir} to user PATH`);
        lines.push("  (restart your terminal for PATH to take effect)");
      } else {
        lines.push(`  ${destDir} already on PATH`);
      }
    } catch (e) {
      lines.push(`  note: could not update PATH automatically: ${e.message}`);
      lines.push(`  add this to your PATH manually: ${destDir}`);
    }
  } else {
    if (!isOnPath(destDir)) {
      lines.push(`  add to your shell profile to put hm on PATH:`);
      lines.push(`  export PATH="$PATH:${destDir}"`);
    } else {
      lines.push(`  ${destDir} already on PATH`);
    }
  }

  lines.push("", "done. you can now run `hm` from anywhere.");
  return { status: "success", output: lines.join("\n") };
}

async function cmdUninstall() {
  const dest    = binaryDest();
  const destDir = configDir();
  const lines   = [];

  if (!fs.existsSync(dest)) {
    return { status: "error", error: `hm binary not found at ${dest}\nnothing to uninstall.` };
  }

  fs.unlinkSync(dest);
  lines.push(`✓ removed ${dest}`);

  if (process.platform === "win32") {
    try {
      removeFromPathWindows(destDir);
      lines.push(`✓ removed ${destDir} from user PATH`);
    } catch (e) {
      lines.push(`  note: could not update PATH: ${e.message}`);
    }
  }

  lines.push("", "uninstalled. your plugins and config are untouched.");
  return { status: "success", output: lines.join("\n") };
}

async function cmdPlugin(args) {
  const sub  = args[0];
  const rest = args.slice(1);

  switch (sub) {

    case undefined:
    case "list": {
      let registry;
      try { registry = await fetchRegistry(); }
      catch (e) { return { status: "error", error: `could not reach registry: ${e.message}` }; }

      const installed = new Set(listInstalledPlugins());
      const lines = ["available plugins (✓ = installed):", ""];
      for (const p of registry.plugins || []) {
        const mark = installed.has(p.name) ? "✓" : " ";
        lines.push(`  ${mark} ${p.name.padEnd(16)} v${p.version}  ${p.description}`);
      }
      return { status: "success", output: lines.join("\n") };
    }

    case "info": {
      const name = rest[0];
      if (!name) return { status: "error", error: "usage: plugin info <name>" };

      let registry;
      try { registry = await fetchRegistry(); }
      catch (e) { return { status: "error", error: `could not reach registry: ${e.message}` }; }

      const entry = (registry.plugins || []).find((p) => p.name === name);
      if (!entry) return { status: "error", error: `'${name}' not found in registry` };

      const lines = [
        `name:        ${entry.name}`,
        `version:     ${entry.version}`,
        `description: ${entry.description}`,
      ];

      try {
        const toml = await fetchInstallToml(name);
        const deps = toml?.install?.plugins || [];
        const run  = toml?.install?.run     || [];
        if (deps.length) lines.push(`dependencies: ${deps.join(", ")}`);
        if (run.length)  lines.push(`build steps:  ${run.join(", ")}`);
      } catch { /* no install.toml */ }

      const installed = fs.existsSync(pluginDir(name));
      lines.push(``, `status: ${installed ? "installed" : "not installed"}`);
      return { status: "success", output: lines.join("\n") };
    }

    case "add": {
      const name = rest[0];
      if (!name) return { status: "error", error: "usage: plugin add <name>" };

      let registry;
      try { registry = await fetchRegistry(); }
      catch (e) { return { status: "error", error: `could not reach registry: ${e.message}` }; }

      const entry = (registry.plugins || []).find((p) => p.name === name);
      if (!entry) {
        return {
          status: "error",
          error: `'${name}' not found in registry.\nrun 'plugin list' to see available plugins.`,
        };
      }

      if (fs.existsSync(pluginDir(name))) {
        return {
          status: "success",
          output: `'${name}' is already installed.\nuse 'plugin remove ${name}' first to reinstall.`,
        };
      }

      try {
        const lines = await installPlugin(name);
        lines.push("", `done. press Ctrl+R to reload plugins.`);
        return { status: "success", output: lines.join("\n") };
      } catch (e) {
        return { status: "error", error: `install failed: ${e.message}` };
      }
    }

    case "remove": {
      const name = rest[0];
      if (!name) return { status: "error", error: "usage: plugin remove <name>" };
      if (name === "hm") return { status: "error", error: "cannot remove the core hm plugin" };

      const dir = pluginDir(name);
      if (!fs.existsSync(dir)) return { status: "error", error: `'${name}' is not installed` };

      fs.rmSync(dir, { recursive: true, force: true });
      return { status: "success", output: `removed '${name}'. press Ctrl+R to reload.` };
    }

    case "disable": {
      const name = rest[0];
      if (!name) return { status: "error", error: "usage: plugin disable <name>" };
      if (name === "hm") return { status: "error", error: "cannot disable the core hm plugin" };

      const dir    = pluginDir(name);
      const disDir = dir + ".disabled";
      if (!fs.existsSync(dir)) {
        if (fs.existsSync(disDir)) return { status: "error", error: `'${name}' is already disabled` };
        return { status: "error", error: `'${name}' is not installed` };
      }
      fs.renameSync(dir, disDir);
      return { status: "success", output: `disabled '${name}'. press Ctrl+R to reload.` };
    }

    case "enable": {
      const name = rest[0];
      if (!name) return { status: "error", error: "usage: plugin enable <name>" };

      const dir    = pluginDir(name);
      const disDir = dir + ".disabled";
      if (fs.existsSync(dir))    return { status: "error", error: `'${name}' is already enabled` };
      if (!fs.existsSync(disDir)) return { status: "error", error: `'${name}' is not installed` };

      fs.renameSync(disDir, dir);
      return { status: "success", output: `enabled '${name}'. press Ctrl+R to reload.` };
    }

    case "installed": {
      const installed = listInstalledPlugins();
      const disabled  = listDisabledPlugins();
      if (installed.length === 0 && disabled.length === 0) {
        return { status: "success", output: "no plugins installed" };
      }
      const lines = [];
      for (const name of installed) {
        const m = loadLocalManifest(name);
        lines.push(`  ✓  ${name.padEnd(16)} v${m?.plugin?.version || "?"}`);
      }
      for (const name of disabled) {
        lines.push(`  ✗  ${name.padEnd(16)} (disabled)`);
      }
      return { status: "success", output: lines.join("\n") };
    }

    default:
      return {
        status: "error",
        error: `unknown subcommand '${sub}'.\n\nusage: plugin <list|info|add|remove|enable|disable|installed>`,
      };
  }
}

// ── Boot ──────────────────────────────────────────────────────────────────────

async function main() {
  const logFile = require("path").join(require("os").tmpdir(), "hm-plugin.log");
  const log = (msg) => require("fs").appendFileSync(logFile, new Date().toISOString() + " " + msg + "\n");

  log("plugin started");
  const raw = require("fs").readFileSync(0, "utf8");
  log("stdin read: " + raw.slice(0, 80));

  let req;
  try {
    req = JSON.parse(raw);
  } catch {
    process.stdout.write(JSON.stringify({ status: "error", error: "invalid JSON request" }));
    return;
  }

  const { command, args = [] } = req;

  try {
    let result;
    switch (command) {
      case "help":      result = await cmdHelp();            break;
      case "version":   result = await cmdVersion();         break;
      case "plugins":   result = await cmdPlugins();         break;
      case "config":    result = await cmdConfig();          break;
      case "install":   result = await cmdInstall();         break;
      case "uninstall": result = await cmdUninstall();       break;
      case "plugin":    result = await cmdPlugin(args);      break;
      default:
        result = { status: "error", error: `unknown command '${command}'` };
    }
    log("writing result for command: " + command);
    process.stdout.write(JSON.stringify(result));
    log("done");
  } catch (e) {
    log("error: " + e.message);
    process.stdout.write(JSON.stringify({ status: "error", error: e.message }));
  }
}

main();