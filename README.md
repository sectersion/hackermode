<div align="center">

```
██╗  ██╗ █████╗  ██████╗██╗  ██╗███████╗██████╗ ███╗   ███╗ ██████╗ ██████╗ ███████╗
██║  ██║██╔══██╗██╔════╝██║ ██╔╝██╔════╝██╔══██╗████╗ ████║██╔═══██╗██╔══██╗██╔════╝
███████║███████║██║     █████╔╝ █████╗  ██████╔╝██╔████╔██║██║   ██║██║  ██║█████╗
██╔══██║██╔══██║██║     ██╔═██╗ ██╔══╝  ██╔══██╗██║╚██╔╝██║██║   ██║██║  ██║██╔══╝
██║  ██║██║  ██║╚██████╗██║  ██╗███████╗██║  ██║██║ ╚═╝ ██║╚██████╔╝██████╔╝███████╗
╚═╝  ╚═╝╚═╝  ╚═╝ ╚═════╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚═╝     ╚═╝ ╚═════╝ ╚═════╝ ╚══════╝
```

**a universal CLI layer. hacker mode for everything.**

[![Rust](https://img.shields.io/badge/built_with-rust-orange?style=flat-square&logo=rust)](https://www.rust-lang.org/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green?style=flat-square)](LICENSE)
[![Status: WIP](https://img.shields.io/badge/status-wip-yellow?style=flat-square)]()

</div>

---

`hm` is a single terminal interface for everything — GitHub, AI, your filesystem, any API. Instead of bouncing between browser tabs and GUI apps, you drop into hacker mode and control it all from the keyboard.

Built in **Rust** (fast, native binary) with plugins in **TypeScript** (easy to write, easy to share).

```
$ hm
```

```
⬡ hackermode  [ home ] [ github ] [ ai ]
┌─────────────────────────────────────────┐ ┌──────────────┐
│ hm > _                                  │ │ plugins      │
│                                         │ │              │
│ hackermode v0.1.0 — press ? for help    │ │ ▶ github     │
│ type a command or use tab to switch     │ │   ai         │
│                                         │ │ ─── keys ─── │
│                                         │ │ Tab  next    │
│                                         │ │ Enter run    │
│                                         │ │ q    quit    │
└─────────────────────────────────────────┘ └──────────────┘
```

---

## install

> **Prerequisites:** [Rust](https://rustup.rs) + [Node.js](https://nodejs.org) (for plugins)

```bash
git clone https://github.com/YOUR_USERNAME/hackermode
cd hackermode
cargo build --release
```

The binary lands at `target/release/hm`. Add it to your PATH:

```bash
# add to your shell profile (.bashrc / .zshrc / PowerShell $PROFILE)
export PATH="$PATH:/path/to/hackermode/target/release"
```

---

## configure

Create `~/.config/hackermode/config.toml`:

```toml
# global env vars passed to all plugins
[env]
GITHUB_REPO = "your-username/your-repo"

# register plugins
[plugins.github]
path = "/path/to/hackermode/plugins/github/dist/index.js"

[plugins.github.env]
GITHUB_TOKEN = "ghp_your_token_here"

[plugins.ai]
path = "/path/to/hackermode/plugins/ai/dist/index.js"

[plugins.ai.env]
ANTHROPIC_API_KEY = "sk-ant-your_key_here"
# OPENAI_API_KEY = "sk-your_key_here"  # alternative
```

---

## usage

### TUI mode (default)

```bash
hm
```

- `Tab` / `Shift+Tab` — cycle between plugins
- `Enter` — run a command
- `↑` / `↓` — scroll output
- `q` — quit

### headless mode

```bash
hm github prs
hm github prs --repo torvalds/linux
hm github issues
hm github status

hm ai ask "what is a monad"
hm ai summarize "$(cat some_file.txt)"
```

---

## plugins

### built-in: github

```
prs [--repo owner/repo]     list open pull requests
issues [--repo owner/repo]  list open issues
status                      current branch + git log
```

### built-in: ai

```
ask <prompt>        one-shot question (Claude or GPT-4o)
summarize <text>    summarize in bullet points
```

Defaults to Claude if `ANTHROPIC_API_KEY` is set, falls back to OpenAI.

---

## build your own plugin

A plugin is a TypeScript file that reads a JSON request from stdin and writes a JSON response to stdout. The SDK handles all the wiring.

```bash
mkdir -p plugins/myplugin
cd plugins/myplugin
npm init -y
npm install @hackermode/plugin-sdk
```

```typescript
// plugins/myplugin/index.ts
import { boot, ok, fail, Plugin } from "@hackermode/plugin-sdk";

const plugin: Plugin = {
  name: "myplugin",
  description: "does something cool",
  commands: {
    hello: async (args, env) => {
      return ok(`hello, ${args[0] ?? "world"}`);
    },
  },
};

boot(plugin);
```

Register it in `config.toml`:

```toml
[plugins.myplugin]
path = "/path/to/plugins/myplugin/dist/index.js"
```

That's it. `hm myplugin hello` works immediately.

---

## architecture

```
hm (Rust binary)
 ├── TUI — ratatui, crossterm
 ├── CLI — clap
 ├── Plugin runner — spawns node, pipes JSON
 └── Config — TOML, ~/.config/hackermode/

plugins/ (TypeScript, run by Node.js)
 ├── github/
 ├── ai/
 └── your-plugin/

plugin-sdk/ (TypeScript library)
 └── boot(), ok(), fail(), Plugin interface
```

The Rust core is intentionally thin — it owns the terminal, the event loop, and the plugin protocol. All integration logic lives in plugins.

---

## roadmap

- [ ] `hm init` — interactive setup wizard
- [ ] Plugin registry / `hm install <plugin>`
- [ ] Streaming output for long-running commands
- [ ] Multi-pane TUI layouts
- [ ] Plugin hot-reload in dev mode
- [ ] `browser` plugin (Playwright)
- [ ] `fs` plugin (fuzzy file search, preview)

---

## contributing

Plugins are the easiest way to contribute — if you've built one, open a PR to add it to the registry.

For core changes, open an issue first.

---

## license

MIT