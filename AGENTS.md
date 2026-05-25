# hackermode — Agent Journal & Architecture

This document is the source of truth for hackermode's design. It captures decisions, open questions, and the build plan. Update it as the project evolves.

## Vision

A CLI for everything. Replace common webapps (email, cloud storage, AI chat, calendars, notes, etc.) with a unified terminal UI. The base ships empty; users assemble their environment from a marketplace of modules, like Arch Linux + the AUR or npm.

## Non-goals (v0)

- No bundled "official" modules. The core is a host; functionality is opt-in.
- No multi-user / server mode. Single-user local app.
- No GUI fallback. Terminal only.

---

## UI / Layout

Built with Bubble Tea + lipgloss. Inspired by opencode.

```
┌─────────────────────────────────────────────────────────┐
│ [tab1] [tab2] [tab3] +                                  │  ← tablist (1 row)
├──────────────────────────────────────────┬──────────────┤
│                                          │              │
│   output box (scrollback, wraps)         │  side panel  │
│                                          │  - plugins   │
│                                          │  - notifs    │
│                                          │              │
├──────────────────────────────────────────┤              │
│ > input box (autocomplete inline + popup)│              │
├──────────────────────────────────────────┴──────────────┤
│ ?:help  tab:complete  ctrl+b:toggle panel  q:quit       │  ← statusline
└─────────────────────────────────────────────────────────┘
```

### Regions

- **Tablist** (top, 1 row): one tab per open module session. `+` opens a launcher to pick a module. Tabs are like browser tabs — multiple sessions of the same module allowed. Keybinds: `ctrl+t` new, `ctrl+w` close, `ctrl+tab` / `ctrl+shift+tab` cycle, `alt+1..9` jump.
- **Main area**: split vertically into output (top, flex) and input (bottom, 1–3 rows, autosize).
  - **Output**: append-only scrollback for the active tab. Supports ANSI styling from modules. PgUp/PgDn or `ctrl+u`/`ctrl+d` to scroll.
  - **Input**: line editor with history (`up`/`down`), inline autocomplete ghost text, popup suggestions on `tab`. Modules supply completion sources.
- **Side panel** (right, 10–15% of width, default 12%): toggleable with `ctrl+b`. Hidden by default on narrow terminals (<100 cols). Shows:
  - Loaded modules (status: running/idle/error)
  - Recent notifications (toasts that persist here)
  - Module-contributed widgets (optional, future)
- **Statusline** (bottom, 1 row): contextual keybind hints based on focus + active module. Modules can append hints.

### Focus model

One focus target at a time: input box (default), output (scroll mode), tablist, or side panel. `esc` returns to input.

### Theming

lipgloss-based theme struct in `internal/tui/theme`. One default dark theme to start. Theme is loaded from config; modules read theme tokens via the host API (no direct lipgloss imports across the boundary).

---

## Architecture

### Process model

The host is a single Go binary (`hackermode`). Modules are **standalone Go binaries** that the host launches as subprocesses. Communication is always over stdio, but a module declares one of two **runtime modes** in its manifest:

1. **`stream` mode** (simple) — the module writes plain text (and ANSI escapes) to stdout and reads commands from stdin. The host wraps that stream into the active tab's output box. Easiest to author; ideal for REPLs, chat-style tools, log tailers, scripted workflows.
2. **`tui` mode** (rich) — the module is itself a Bubble Tea program. The host hands it a dedicated PTY for the tab's main area, forwards key events and resizes, and renders the module's frames into that region. The host still owns the tablist, side panel, and statusline.

Both modes share the same **control channel**: a JSON-RPC 2.0 line-delimited stream on a separate fd (fd 3 — set up by the host) used for capability negotiation, completions, notifications, secrets, and keybind hints. This keeps the user-facing I/O clean while giving the host structured metadata.

```
        ┌──────────────── host ────────────────┐
        │                                      │
        │  stdin  ─────────► module stdin      │  ← user input (stream mode)
        │  stdout ◄───────── module stdout     │  ← rendered output
        │  fd 3   ◄────────► module fd 3       │  ← JSON-RPC control
        │                                      │
        └──────────────────────────────────────┘
```

In `tui` mode, stdin/stdout are wired through a PTY allocated per tab so Bubble Tea sees a real terminal; the host multiplexes that PTY into the tab's main area and handles tab switching by suspending input delivery (the module keeps running but receives no keys until refocused).

**Why subprocess over Go plugins (`.so`):**
- Go plugins are Linux/macOS only and brittle across Go versions.
- Crashes in a module can't take down the host.
- Modules can be killed/restarted/sandboxed independently.
- Easy to swap to other languages later — protocol is language-agnostic. Stream-mode modules can even be shell scripts or Python.
- Distribution is a single static binary per module per platform.

**Why not WASM (yet):** OAuth, native keychain access, and arbitrary network protocols (IMAP) are painful in WASM. Revisit later as an optional sandboxed runtime.

### Lifecycle

1. Host starts, reads `~/.config/hackermode/config.toml` and module registry at `~/.local/share/hackermode/modules/`.
2. User opens a tab → host spawns the module binary if not running, performs handshake, sends `init`.
3. Module returns its capabilities (commands, completions, widgets, keybinds).
4. User input in tab is forwarded as RPC calls; module returns output frames asynchronously.
5. On tab close or idle timeout, host sends `shutdown`; module exits gracefully (SIGTERM after 5s).

### RPC surface (control channel, fd 3)

Shared by both modes. I/O for the user goes via stdio (stream) or PTY (tui), not RPC.

Host → Module:
- `init({tab_id, mode, theme, host_version, size:{w,h}})` → `{module_info, commands, completers, keybinds}`
- `complete({tab_id, text, cursor})` → `{suggestions[]}`
- `resize({tab_id, w, h})`
- `focus({tab_id, focused: bool})` — module may pause animations/polling when unfocused
- `shutdown()`

Module → Host:
- `set_status({tab_id, text})` — statusline contribution
- `set_title({tab_id, text})` — tab label
- `notify({level, title, body})` — toast + side panel entry
- `panel({tab_id, widget})` — contribute a side-panel widget (text/list)
- `request_secret({namespace, key})` → secret value (host fetches from keychain, prompts user if missing)
- `http({req})` → response (host mediates so it can enforce permissions; v1 may bypass for trusted modules)

Stream-mode-only:
- (none — module just reads stdin / writes stdout)

TUI-mode-only:
- Host forwards key events and resize signals through the PTY natively; no extra RPC needed for I/O.

A reference Go SDK lives at `pkg/module/` so module authors choose their mode:

```go
// stream mode
module.RunStream(func(ctx context.Context, h module.Host, in io.Reader, out io.Writer) error {
    // read stdin, write stdout
})

// tui mode
module.RunTUI(func(ctx context.Context, h module.Host) tea.Model {
    return myModel{host: h}
})
```

### Permissions

Each module's manifest declares capabilities. v0 keeps it simple: capabilities are advisory and shown to the user at install time. Sandboxing (seccomp/landlock, network namespaces) is post-v1.

Capabilities (initial set):
- `network` — outbound HTTP/sockets
- `filesystem:read` / `filesystem:write` — paths user grants
- `secrets` — read/write entries under module's namespace in keychain
- `exec` — run subprocesses
- `clipboard` — read/write system clipboard

---

## Modules

### Manifest (`hackermode.toml`)

```toml
[module]
id = "acme.email"            # reverse-DNS, globally unique in registry
name = "Email"
version = "0.3.1"             # semver
description = "IMAP/SMTP email client"
author = "Acme"
license = "MIT"
homepage = "https://github.com/acme/hackermode-email"

[entry]
# Either a binary name (resolved per-platform) or a build manifest
binary = "hackermode-email"
mode = "tui"        # "stream" | "tui"
# Optional: a stream-mode module can declare it accepts ANSI on stdout
ansi = true

[capabilities]
required = ["network", "secrets"]
optional = ["clipboard"]

[dependencies]
"acme.oauth" = "^1.0.0"

[platforms]
# Which platforms the module ships prebuilt binaries for
supported = ["linux/amd64", "linux/arm64", "darwin/arm64", "darwin/amd64"]
```

### Layout on disk

See "Layout on disk" under Config below.

### Dependency resolution

- Semver with caret/tilde/range constraints (`^`, `~`, `>=`).
- Resolver: **PubGrub** (Cargo/uv/Bundler-style). Use `github.com/mircearoata/pubgrub-go` or vendor an equivalent. Better error messages than naive backtracking from day one.
- Conflict policy: **hard fail** with a clear "X requires A^1, Y requires A^2" message. No silent duplicates / multiple versions of the same module loaded.
- Each top-level module declares its own deps; the host computes one global graph for the active environment.
- Per-platform binary hashes are pinned in the lockfile so a single lockfile works across Linux/macOS/arm/amd.

---

## Manifests, lockfiles & environments

This is the install model. Inspired by Cargo / npm / Nix flakes. **Manifest-first from v0** — imperative `install <id>` commands are sugar that edit the manifest and re-resolve, never a separate state-mutation path.

### Two files

- **`hackermode.toml`** — declarative, hand-edited. What you *want*.
- **`hackermode.lock`** — generated. Exact versions + per-platform hashes. Commit this.

### Manifest example

```toml
[hackermode]
version = "0.1"                    # manifest schema version

[profile]
name = "work"
description = "Daily driver setup"
extends = ["base"]                 # optional: inherit modules from another profile

[modules]
"acme.email"     = "^0.3"
"acme.ai-chat"   = "~1.2.0"
"acme.oauth"     = "^1.0"
"corp.internal"  = { git = "https://github.com/corp/hackermode-internal", rev = "v0.4.1" }
"local-thing"    = { path = "../my-module" }       # dev-only, not locked
"alt-mod"        = { registry = "alt", version = "^2" }

[modules.config."acme.email"]      # non-sensitive defaults only; secrets go to keychain
default_account = "me@example.com"

[ui]
theme = "tokyo-night"              # themes are modules too
side_panel_width = 14
```

### Manifest scopes (resolution order)

1. **Project** — `./hackermode.toml` in cwd or any ancestor (git-style discovery). Per-repo / per-directory environments.
2. **Profile** — `~/.config/hackermode/profiles/<name>.toml`. Activated via `--profile <name>` or `HACKERMODE_PROFILE`.
3. **System** — `~/.config/hackermode/hackermode.toml`. Default fallback when no project file and no profile selected.

Exactly one manifest is active per `hackermode` invocation.

### Lockfile

```toml
[meta]
manifest_hash = "sha256:..."
generated     = "2026-05-20T12:34:56Z"
hackermode    = ">=0.1"

[[module]]
id      = "acme.email"
version = "0.3.4"
source  = "registry"
sha256  = "abc123..."
[[module.binaries]]
platform = "linux/amd64"
sha256   = "def456..."
url      = "https://.../email-0.3.4-linux-amd64.tar.gz"
[[module.binaries]]
platform = "darwin/arm64"
sha256   = "..."
url      = "..."
```

Every install verifies hashes; mismatch = abort. Path deps don't go in the lockfile (warn loudly). Git deps lock to commit SHA.

### Environments — shareable bundles of modules

An **environment** is a published profile: a manifest plus optional metadata, distributed through the same registry as modules. Lets people ship setups/workflows ("ai-power-user", "devops-toolkit", "journalism-suite") that someone else can install with one command.

An environment package is a tarball containing:

```
my-env-1.2.0.tar.gz
├── hackermode.toml       # the manifest itself (modules + UI prefs)
├── hackermode.lock       # optional but recommended for reproducibility
├── README.md             # describes the workflow
├── screenshots/          # optional
└── env.toml              # environment metadata (id, version, author, tags)
```

`env.toml`:

```toml
[environment]
id          = "alex.ai-power-user"
name        = "AI Power User"
version     = "1.2.0"
description = "Multi-provider AI chat, prompt library, code review"
author      = "alex"
license     = "MIT"
tags        = ["ai", "productivity"]
homepage    = "https://github.com/alex/hackermode-ai-power-user"
```

Resolution: registry distinguishes environments from modules by namespace prefix or a `kind` field in the index. Both live in the same registry; both are versioned the same way.

### Installing an environment

```
hackermode env install alex.ai-power-user             # pulls into a new profile named after the env
hackermode env install alex.ai-power-user --as work   # custom profile name
hackermode env install ./local-env.toml               # from local file
hackermode env install https://.../env.tar.gz        # from URL
```

This:
1. Downloads + verifies the env tarball.
2. Copies its `hackermode.toml` to `~/.config/hackermode/profiles/<name>.toml`.
3. Resolves + installs all modules (using the bundled lockfile if present, otherwise re-resolves).
4. Switches the active profile (unless `--no-activate`).

Updates: `hackermode env update <id>` re-pulls the env, diffs against the local profile, and prompts on conflicts (the user may have customized).

### Publishing an environment

`hackermode env publish` in a directory with `env.toml` + `hackermode.toml`:
1. Validates the manifest resolves cleanly.
2. Optionally regenerates the lockfile.
3. Uploads to registry (or prepares a PR to the git-backed registry index).

### Capability surface for environments

Installing an environment is **strictly equivalent** to installing the modules it lists — no extra trust granted to the env publisher. The capability prompt iterates over the union of modules' declared capabilities. This keeps env publishers from being a privilege-escalation vector ("install this env" can't smuggle in unexpected access; the user still sees every module's caps).

### Why this matters

- **Dotfiles**: drop `hackermode.toml` in your dotfiles repo, `hackermode sync` reinstalls everything on a new machine.
- **Onboarding**: a company publishes a `team` environment; new hires `hackermode env install corp.team` and get the same setup.
- **Sharing**: blog post "my hackermode setup" links a published env; readers install with one command.
- **Reproducible bug reports**: attach `hackermode.lock`.

---

## Marketplace

### Hosting

Two-stage plan:
1. **v0 (now): Git-backed registry.** A single repo (e.g. `hackermode/registry`) with a `modules/<id>.toml` per module containing version list + tarball URLs (released as GitHub releases on the module's own repo). The CLI clones/pulls this repo for the index. Zero infra, signed by GitHub commits.
2. **v1: Cloudflare Worker + R2.** A small Worker exposes `/index`, `/module/{id}`, `/module/{id}/{version}/download` endpoints; R2 stores tarballs. Publishing is a `POST` with a token. The Worker also serves the static index for the CLI to cache. Self-hosting is supported by pointing the CLI at a different base URL in config.

### Package format

A `.tar.gz` containing:
- `hackermode.toml`
- platform-specific binary (or a directory of them)
- `LICENSE`, `README.md` (optional)
- `SHA256SUMS` (signed in v1)

### CLI commands

Manifest-aware (operate on the active manifest + lockfile):

- `hackermode init` — scaffold `hackermode.toml` in cwd
- `hackermode install` — resolve + install + write lock (no args: install everything in manifest)
- `hackermode install <id>[@version]` — add to manifest + re-resolve
- `hackermode remove <id>` — remove from manifest + re-resolve
- `hackermode update [<id>]` — bump within manifest constraints, rewrite lock
- `hackermode upgrade [<id>]` — bump constraints themselves (interactive)
- `hackermode lock` — re-resolve without installing
- `hackermode sync` — install exactly what's in lock; **no resolution**. CI / fresh-machine command.
- `hackermode why <id>` — explain why a module is in the graph
- `hackermode outdated` — what could be upgraded
- `hackermode list` — installed modules in active environment
- `hackermode search <query>` — search registry (modules + envs)

Profiles & environments:

- `hackermode profile use <name>` / `new` / `list` / `delete`
- `hackermode env install <id|path|url> [--as <name>]`
- `hackermode env update <id>`
- `hackermode env publish` — publish current dir as an environment
- `hackermode env export [--lock]` — write current setup as a redistributable env tarball

Author / dev:

- `hackermode link <path>` — register a local module for `path = ...` deps without publishing
- `hackermode new module <name>` — scaffold a module project (manifest, SDK wired, GH Actions for releases)
- `hackermode publish` — publish current module dir to the registry

Headless / scripting:

- `hackermode run <id> [-- args...]` — invoke a module without the TUI; stdio passes through. Every module is also a regular CLI tool, scriptable in shell pipelines.

Inside the TUI: a built-in `marketplace` tab for browsing/installing modules and environments.

### Trust

- v0: install warns about declared capabilities; user confirms. **Capability diff on update**: if a module update *adds* capabilities (e.g. previously didn't need `secrets`, now does), prompt explicitly. Catches hijacked packages.
- v1: signed releases (sigstore/cosign or minisign), pinned publisher keys in registry index. Require modules be built via GitHub Actions with attestation; registry verifies binary matches a tagged commit.
- Lockfile hashes every binary. `sync` refuses on mismatch.
- Mirror-friendly: registry index is static files; anyone can mirror or self-host. Don't let the Worker become a chokepoint.

---

## Config

Two layers:

1. **Manifest** (`hackermode.toml`) — declares modules, UI prefs, non-sensitive module config defaults. Shareable. See "Manifests, lockfiles & environments" above.
2. **Local user config** (`~/.config/hackermode/config.toml`) — machine-local settings that don't belong in a shareable manifest: registry endpoint, default profile, keymap overrides, telemetry opt-out (always off, no opt-in).

```toml
# ~/.config/hackermode/config.toml
[hackermode]
default_profile = "personal"

[marketplace]
registry = "https://registry.hackermode.dev"
# or: registry = "git+https://github.com/hackermode/registry"
mirrors  = []

[keymap]                              # any host keybind remappable from day one
"toggle_panel"   = "ctrl+b"
"command_palette" = "ctrl+k"
"new_tab"        = "ctrl+t"
```

**Module configuration** has two paths:
- Non-sensitive defaults declared in the manifest under `[modules.config.<id>]` (shareable).
- Sensitive values (API keys, OAuth tokens) stored in the **OS keychain** via `github.com/zalando/go-keyring` under namespace `hackermode/<module-id>/<key>`. Modules request these via the `request_secret` RPC. Never written to manifest or config files.

**Module config schema**: a module may declare a config schema in its manifest; the host renders a settings UI from it (a per-module settings tab) so authors don't roll their own forms.

### Layout on disk

```
~/.local/share/hackermode/
├── modules/
│   ├── acme.email/
│   │   ├── 0.3.4/
│   │   │   ├── hackermode.toml
│   │   │   ├── hackermode-email
│   │   │   └── LICENSE
│   │   └── current -> 0.3.4
│   └── acme.oauth/...
├── envs-cache/                         # downloaded env tarballs
└── registry-cache/                     # cached registry index

~/.config/hackermode/
├── config.toml                         # local user config
├── hackermode.toml                     # system-default manifest
├── hackermode.lock
└── profiles/
    ├── work.toml
    ├── personal.toml
    └── work.lock                       # per-profile lock

~/.cache/hackermode/                    # logs, autocomplete caches, sessions
```

---

## Host extension points

The host is a substrate; modules extend it through a fixed set of contracts. These are the **hooks**. Everything a module does — UI, behavior, automation — flows through one of them. Designing this carefully now beats retrofitting later (VS Code's success is largely about a well-designed extension API; Atom's failure was the inverse).

### UI hooks

| Hook | What a module contributes | When |
|---|---|---|
| **Commands** | Palette entries: `{id, title, hint, tags, when?, keybind?, arg?}` | Manifest (static) + RPC `register_command` (dynamic) |
| **Completers** | Per-tab and global completion sources for the input box | RPC `register_completer({scope, kinds})` |
| **Keybinds** | Named bindings the user can remap | Manifest `[[keybinds]]` |
| **Side-panel widgets** | Live mini-views (unread count, sync status, current track) | RPC `panel({tab_id, widget})` |
| **Statusline hints** | Contextual keybind tips | RPC `set_status_hints` |
| **Notifications** | Toasts + persistent history; can reference action commands | RPC `notify` |
| **Themes** | Theme tokens via `kind = "theme"` modules | Manifest (theme-mode module) |
| **Settings schema** | Declarative config → host-rendered settings tab | Manifest `[config_schema]` |
| **Tab launchers** | Entries in the `+` menu and palette to open new module sessions | Manifest `[[launchers]]` |

### System hooks

| Hook | What a module subscribes to / requests | Mechanism |
|---|---|---|
| **Events** | `tab.opened`, `tab.closed`, `tab.focused`, `tab.suspended`, `tab.resumed`, `notification.*`, `network.online`, `secret.requested`, `command.invoked` | RPC `subscribe(events)` |
| **Inter-module RPC** | Methods provided/consumed | Manifest `[provides]` / `[consumes]`, host routes calls |
| **Lifecycle hooks** | `on_install`, `on_update`, `on_uninstall`, `on_first_run` | SDK entry points; module is spawned just to run the hook |
| **Pre/post command hooks** | Intercept any command invocation (audit, confirm, dry-run) | RPC `register_command_hook({pattern, kind})`, capability-gated |
| **Capability requests** | Filesystem, secrets, clipboard, exec, network | Manifest declares; host enforces |

### Commands in detail

The single most important extension point. Modules expose commands two ways.

**Static (manifest)** — visible in the palette before the module is spawned. The host lazy-launches the module on first invocation; cold modules don't slow down discovery.

```toml
[[commands]]
id      = "email.compose"          # namespaced by module id
title   = "Compose Email"
hint    = "Open new draft"
tags    = ["mail", "write", "new"]
keybind = "ctrl+alt+m"              # default; user can override
when    = "tab.module == 'acme.email' || always"
icon    = "✉"

[[commands]]
id  = "email.search"
title = "Search Mailbox"
arg = { name = "query", placeholder = "from:alice subject:..." }
```

**Dynamic (RPC)** — registered at runtime, deduped by ID. Useful for state-driven commands (one entry per email account, per open file, etc.):

- `register_command({id, title, hint, tags, when})`
- `unregister_command(id)`
- `update_command(id, fields)`
- `set_command_visibility(id, visible)`

**Invocation flow**:

1. User opens palette, picks `email.compose`.
2. Host resolves command → module `acme.email`.
3. If module not running, host spawns it, waits for handshake.
4. Host sends `invoke({command_id, args})` over the control channel.
5. Module returns a structured response — one of:
   - `{open_tab: "email.compose"}` — open a module tab.
   - `{prompt_arg: {name, placeholder}}` — palette chains into an argument prompt.
   - `{notify: {...}}` — fire-and-forget.
   - `{output: "..."}` — print to the current tab's output buffer.
   - `{noop: true}` — nothing else needed.
6. Palette closes (unless `prompt_arg` chained it).

### When-clauses

Borrowed from VS Code. Cheap, declarative visibility predicates evaluated against a small context. Prevents palette pollution.

**Context variables**:
- `tab.module` — module ID of focused tab, or `""`.
- `tab.count` — number of open tabs.
- `panel.visible` — bool.
- `network.online` — bool.
- `always` — `true`.
- Module-defined: modules can publish state keys (`when = "email.unread > 0"`).

Grammar (full target): identifiers, string/number literals, `==`, `!=`, `<`, `>`, `&&`, `||`, `!`, parentheses. No function calls. Implementation: tiny recursive-descent parser; reject anything unparseable at install time.

**Phase B status: stub grammar.** Only these forms are accepted; everything else (logical connectives, negation, parentheses) defers to Phase E. This keeps Phase B unblocked without committing to grammar details before real modules exist to inform them:

- `always` — `true`
- `<ident> == "<string>"` — e.g. `tab.module == "acme.email"`
- `<ident> != "<string>"`
- `<ident> > <number>` and `<ident> < <number>` — e.g. `tab.count > 0`
- An empty / missing `when` is `always`.

Unparseable expressions are accepted at install time (with a warning logged) but evaluate to `false`, so commands using future grammar simply hide rather than crash. Phase E replaces the stub with the full parser; existing manifests keep working.

### Namespacing & conflicts

- Command IDs: `<module-id>.<command>` (e.g. `acme.email.compose`).
- Two modules can't share a command ID — host refuses the second registration with a clear error.
- Default-keybind conflicts detected at install time: host prompts user to pick. User-config keybinds always win over module defaults.
- Provides/consumes RPC names: `<module-id>.<method>`; host enforces declared `[provides]` matches actual `register_rpc` calls.

### Pre/post command hooks

Lets cross-cutting modules wrap invocations. Examples: audit log, confirmation prompts, dry-run, telemetry-blocker. Capability-gated (`hook:commands`).

```toml
[[command_hooks]]
pattern = "email.*"      # globs supported
kind    = "pre"          # pre | post
require_confirm = true   # host shows a confirm dialog before running
```

Hook order is install order; host runs all `pre` hooks → command → all `post` hooks. Any `pre` hook can veto.

### Why this matters

- **Discovery** — palette becomes the front door: hit `ctrl+p`, see everything the env can do.
- **Scriptability** — same registry powers slash commands and headless mode (`hackermode invoke email.compose`).
- **Power-user surface** — one keybind config, one settings tab format, one notification format. Authors don't reinvent.
- **Lazy spawning** — modules don't pay startup cost until invoked.
- **Composability** — themes, completers, command hooks are all just modules.

---

## Cross-cutting features

Things that don't belong to one subsystem but shape the whole product.

### Headless mode

`hackermode run <id> [-- args...]` launches a module without the TUI: stdio passes straight through. Stream-mode modules become regular CLI tools usable in shell pipelines (`hackermode run acme.ai -- "summarize" < file.txt`). TUI-mode modules attach to the current terminal directly. This makes every module valuable even outside the host.

### Inter-module RPC

Modules can expose RPC methods that other modules call (mediated by the host with capability checks). Enables shared infrastructure — e.g. a single `acme.oauth` module that `email`, `calendar`, `drive` all depend on. Manifest declares both the RPC methods provided and the modules consumed:

```toml
[provides]
rpc = ["oauth.authorize", "oauth.token"]

[consumes]
"acme.oauth" = ["authorize", "token"]
```

Host enforces that consumed methods exist in the resolved graph and that the calling module has permission.

### Event bus

Host emits events (`tab.opened`, `tab.closed`, `tab.focused`, `notification.*`, `network.online`, `secret.requested`). Modules subscribe via the SDK. Enables cross-cutting modules: do-not-disturb, presence/status, activity log, focus timer.

### Command palette (`ctrl+k`)

Fuzzy search across:
- All modules' commands (per-module + global)
- Open tabs (jump-to)
- Recent notifications
- Marketplace (search modules / environments inline)
- Host commands (toggle panel, new tab, switch profile)

This is the single feature that makes power users love a tool. Keymap remappable.

### Structured output frames (post-v1)

A small frame protocol modules can emit instead of raw ANSI: `{kind: "table", rows: ...}`, `{kind: "list"}`, `{kind: "kv"}`, `{kind: "progress"}`, `{kind: "image", protocol: "kitty"|"sixel"}`. Host renders consistently across modules. Authors can still emit raw ANSI for full control. Optional, additive.

### Reactive side-panel widgets

Modules pin live widgets to the side panel via the `panel` RPC: unread counts, sync status, current track, build state. First-class API, not just notifications.

### Themes are modules

Themes ship through the registry as modules with `kind = "theme"` in their manifest. Same install/version/distribute path. No special-casing.

### Remappable keymap from day one

Every host keybind reads from `[keymap]` in `config.toml`. Modules contribute their own keybinds at `init` time; the host merges them, detects conflicts, and exposes them in the keymap config. Retrofitting this later is famously painful — non-negotiable from v0.

### Notification history

Built-in tab (alongside `marketplace`) for browsing past notifications, since side-panel toasts are transient. Filter by module, level, time.

### Mouse support

Bubble Tea supports it; use it. Tab close buttons, panel toggle, scrollback, command palette — all clickable. Keyboard remains primary; mouse is additive.

### Onboarding

First run with no manifest: host opens the `marketplace` tab as a guided onboarding experience ("install your first module" or "install a starter environment"). Side panel becomes a "what would you like to do?" prompter when no tabs are open.

### Module config UI

Modules declare a config schema in their manifest. The host renders a settings tab from it — uniform UX, no every-author-rolls-their-own. Sensitive fields route to keychain automatically.

### Session persistence

Tabs persist across restarts via a session file in cache. Modules opt in by responding to `serialize` / `restore` RPC. Default behavior: reopen tabs in their last-known state, but with empty scrollback.

---

## Repository layout (target)

```
hackermode/
├── main.go                        # entry, flag parsing, calls cmd
├── cmd/
│   ├── root.go                    # `hackermode` (launches TUI)
│   ├── run.go                     # headless mode
│   ├── install.go
│   ├── remove.go
│   ├── update.go
│   ├── upgrade.go
│   ├── sync.go
│   ├── lock.go
│   ├── why.go
│   ├── outdated.go
│   ├── init.go
│   ├── search.go
│   ├── list.go
│   ├── link.go
│   ├── publish.go
│   ├── new.go                     # `hackermode new module ...`
│   ├── profile.go                 # subcommands: use/new/list/delete
│   └── env.go                     # subcommands: install/update/publish/export
├── internal/
│   ├── tui/
│   │   ├── app.go                 # root bubbletea Model
│   │   ├── layout.go              # region computation
│   │   ├── tabs/
│   │   ├── input/
│   │   ├── output/
│   │   ├── sidepanel/
│   │   ├── statusline/
│   │   ├── palette/               # ctrl+k command palette
│   │   ├── keymap/                # remappable keybinds
│   │   └── theme/
│   ├── modules/
│   │   ├── manager.go             # lifecycle, registry of running modules
│   │   ├── rpc.go                 # JSON-RPC framing (control channel)
│   │   ├── pty.go                 # PTY allocation for tui-mode modules
│   │   ├── stream.go              # stdio plumbing for stream-mode modules
│   │   ├── manifest.go
│   │   ├── permissions.go
│   │   ├── events.go              # event bus
│   │   └── intermod.go            # inter-module RPC routing
│   ├── manifest/                  # hackermode.toml + lockfile
│   │   ├── parse.go
│   │   ├── lock.go
│   │   ├── resolver.go            # PubGrub-based
│   │   └── profile.go
│   ├── env/                       # environments (shareable bundles)
│   │   ├── pack.go
│   │   ├── install.go
│   │   └── publish.go
│   ├── marketplace/
│   │   ├── client.go              # registry HTTP/git client
│   │   ├── installer.go
│   │   └── verify.go              # hash + signature verification
│   ├── config/
│   ├── secrets/
│   └── paths/                     # XDG path helpers
├── pkg/
│   └── module/                    # public SDK for module authors
│       ├── module.go              # RunStream / RunTUI entry points
│       ├── rpc.go                 # control-channel client
│       ├── events.go
│       └── examples/
│           ├── echo-stream/
│           └── echo-tui/
├── registry/                      # tooling for the registry repo (separate or submodule)
└── docs/
```

---

## Build plan

### Phase A — TUI shell (no modules) ✅ done
- [x] Root Bubble Tea model with the four regions and resize handling.
- [x] Tablist with mock tabs, switching, close, new.
- [x] Output viewport with scrollback.
- [x] Input box with history + a stub completer.
- [x] Side panel toggle (`ctrl+b`), width from config.
- [x] Statusline with focus-aware keybinds.
- [x] Theme system (one dark theme, blue→green brand gradient, Crush-style `╱╱╱` dividers).
- [x] Remappable keymap loaded from `config.toml`.
- [x] Local user config loader (`~/.config/hackermode/config.toml`).
- [x] Mouse support for tabs / panel toggle / scrollback.
- [x] Command palette overlay (`ctrl+p` / `ctrl+k`) with host-command registry stub.

**Known shortcomings to fix during Phase B** (do not skip; documented so they don't get lost):

- **Overlay compositing is a hack**: `overlay()` in `app.go` line-substitutes non-whitespace lines. It doesn't measure ANSI properly, can't handle internal blank lines, and won't compose with multiple overlays (palette + dialog + completion popup). Replace with a real cell-grid compositor (or migrate to lipgloss v2 primitives) before adding more overlays.
- **Ad-hoc event dispatch**: keypresses are routed by hand-ordered `if` chains in `Update`. With modules contributing bindings this becomes unmaintainable. Build a proper **dispatch layer**: input event → keymap lookup → action ID → handler (host or module).
- **Resize handling is partial**: components don't always re-clamp scroll positions on shrink; resize storms aren't debounced. Add a resize debounce (~16ms) and a `Resize()` contract for sub-components.
- **No structured logging**: errors go to `os.Stderr` or vanish. Add a JSONL logger writing to `~/.cache/hackermode/log.jsonl` from the start of Phase B (RPC traffic, command invocations, errors) — Phase B debugging without it will be miserable.
- **Palette "fuzzy" search is substring matching**: replace with proper fuzzy scoring (Smith-Waterman or sublime-style) once we have >20 commands.
- **No unit coverage outside the root model**: tabs / input / output / palette / theme / keymap / config have zero tests. Backfill before they become module-facing API surface.
- **Cross-platform untested**: macOS and Windows builds unverified. PTYs in Phase B will hit this first.

### Phase B — Module runtime + extension API

Goal: by end of Phase B, you can `hackermode link ./examples/echo-tui` and it shows up in the command palette, opens in a tab, contributes side-panel widgets, and survives a host restart.

**Foundation work (carried from Phase A shortcomings)**
- [ ] Cell-grid overlay compositor (or lipgloss v2 migration).
- [ ] Central dispatch layer: `Event → ActionID → Handler`. Host commands and module commands both register here.
- [ ] Structured JSONL logger (`internal/log`) writing to `~/.cache/hackermode/log.jsonl`. Log levels, module ID tags, RPC framing traces gated by env var.
- [ ] Resize debounce + per-component `Resize(w,h)` contract.
- [ ] Backfill unit tests for tabs, input, output, palette, theme, keymap, config.

**Session model**

Every tab owns exactly one `Session`. A session is the *binding* of a tab to whatever is currently driving it — initially the host itself (`module: "host"`), later a module process. When the user picks a module command from a host session, the session **transitions**: the same tab is re-bound to the module that produced the command. Tab launchers (commands that explicitly mean "open a new tab for this module") spawn a brand-new tab + session instead of transitioning the current one. The distinction is per-command in the manifest.

This means:

- The empty / "scratch" tab is not a special case — it is a session with `module = "host"`.
- The palette / slash-command surface always operates on the currently-focused session's context (`tab.module` reflects the bound module).
- Closing a tab tears down its session (and signals the module if any).
- A session may outlive a tab close in Phase E (session persistence); for Phase B, session lifetime equals tab lifetime.

- [ ] Introduce `Session` type (`internal/modules/session.go`): module ID, tab ID, scrollback handle, state (`spawning|running|idle|suspended|errored`), permissions, working context, command history.
- [ ] Refactor `tabs.Tab` to embed a `Session`. Default new-tab session is `module: "host"`.
- [ ] Implement session transitions (`SetModule(...)` style) — same tab, new bound module.
- [ ] Session state machine + transitions tested.

**Module runtime**
- [ ] Manifest parser (`hackermode.toml`) — module section, capabilities, commands, keybinds, widgets, completers, when-clauses.
- [ ] Capability prompt on install + capability diff on update.
- [ ] Module manager (`internal/modules/manager.go`): spawn, handshake, lifecycle, route messages by `tab_id`, graceful shutdown (SIGTERM + 5s grace → SIGKILL).
- [ ] JSON-RPC 2.0 framing on **fd 3** (control channel). Line-delimited.
- [ ] **Stream-mode plumbing**: stdio → output box; input box → stdin; ANSI passthrough.
- [ ] **TUI-mode plumbing**: per-tab PTY (`github.com/creack/pty`), key forwarding, resize forwarding, focus suspend/resume. Module keeps running when unfocused but receives no keys.
- [ ] Background-state policy (see decisions log): unfocused modules run normally; `focus({focused:false})` is advisory. Future: opt-in true suspension.
- [ ] Headless mode: `hackermode run <id> [-- args...]` — stdio passes through, no TUI.

**Host extension points (full surface — see "Host extension points" section)**

- [ ] **Command registry** (`internal/commands`): static (manifest) + dynamic (RPC). Dedup by ID. Module ID namespace (`email.compose`). When-clause evaluation against a small context.
- [ ] **Palette integration**: palette pulls from the registry, not a hard-coded list. Lazy-spawn the owning module on invocation.
- [ ] **Argument prompting**: commands declare `arg`; palette enters input mode after selection (VS Code quick-pick chain).
- [ ] **Slash-command bridge**: `:foo.bar` in the input box invokes command `foo.bar`. Same registry, different surface.
- [ ] **Completers**: module-registered per-tab and global completion sources; merged with stub host completer.
- [ ] **Keybinds**: modules declare default keybinds in manifest. Host merges with user config (user wins). Conflict detection on install.
- [ ] **Side-panel widgets**: modules push live widgets to the side panel via `panel({tab_id, widget})`. Widget kinds: `text`, `kv`, `list`, `progress`.
- [ ] **Statusline contributions**: `set_status`, plus contextual hints from the focused module.
- [ ] **Notifications**: `notify({level, title, body, actions?})`. Toast + persistent side-panel entry. Actions can reference command IDs.
- [ ] **Settings schema**: modules declare config schema; host renders a settings tab from it. Sensitive fields route to keychain automatically.
- [ ] **Tab launchers**: modules expose "open a new <thing>" entries that show up in the `+` menu and palette.
- [ ] **Lifecycle hooks**: `on_install`, `on_update`, `on_uninstall`, `on_first_run` (run via SDK; one-time setup like OAuth flow, schema migration).
- [ ] **Pre/post command hooks**: cross-cutting modules can intercept any command invocation (audit log, confirmations, dry-run mode). Capability-gated.

**System hooks**

- [ ] **Event bus** (`internal/modules/events.go`): emit `tab.opened`, `tab.closed`, `tab.focused`, `tab.suspended`, `tab.resumed`, `command.invoked`, `notification.*`, `network.online`, `secret.requested`. SDK subscribers.
- [ ] **Secrets bridge** via `github.com/zalando/go-keyring`. `request_secret({namespace, key})` RPC. Module gets values lazily; never persisted to manifest/config.
- [ ] **Clipboard RPC**, capability-gated.

**SDK (`pkg/module`)**
- [ ] `RunStream(func(ctx, host, in, out) error)` entry.
- [ ] `RunTUI(func(ctx, host) tea.Model)` entry.
- [ ] `host.RegisterCommand`, `host.Notify`, `host.SetStatus`, `host.SetTitle`, `host.SetPanelWidget`, `host.RequestSecret`, `host.SubscribeEvents`.
- [ ] Code-gen for command IDs (so `email.RegisterCommand("compose", ...)` produces typed constants).
- [ ] Example modules: `examples/echo-stream`, `examples/echo-tui`, `examples/clock-widget` (a side-panel widget demo).

**Author DX**
- [ ] `hackermode link <path>` — register a local module for `path =` deps in `hackermode.toml`.
- [ ] `hackermode new module <name>` — scaffold a module project (manifest with example command/widget, SDK wired).
- [ ] Pretty errors when a manifest is invalid (line numbers, suggestions).

### Phase C — Manifests, lockfiles, marketplace
- [ ] Manifest parser (`hackermode.toml`) + scope resolution (project/profile/system).
- [ ] PubGrub-based resolver.
- [ ] Lockfile (`hackermode.lock`) with per-platform binary hashes.
- [ ] Git-backed registry client + cache.
- [ ] Verify-on-install (hashes; signatures optional in this phase).
- [ ] Capability diff on update.
- [ ] `init`, `install`, `remove`, `update`, `upgrade`, `lock`, `sync`, `why`, `outdated`, `list`, `search` commands.
- [ ] `link`, `new module`, `publish` for authors.
- [ ] `profile use|new|list|delete`.
- [ ] Built-in `marketplace` tab inside the TUI.

### Phase D — Environments
- [ ] Environment package format (`env.toml` + manifest + optional lock).
- [ ] `hackermode env install <id|path|url>`.
- [ ] `hackermode env update <id>` with diff against local profile.
- [ ] `hackermode env publish` / `hackermode env export`.
- [ ] Environments listed in marketplace tab alongside modules, with separate browse view.
- [ ] Onboarding flow on first run: marketplace tab as guided start.

### Phase E — Inter-module & richer UX
- [ ] Inter-module RPC (`provides` / `consumes` in manifest, host routing).
- [ ] Notification history tab.
- [ ] Session persistence (`serialize` / `restore` RPC) — tabs reopen on restart.
- [ ] Themes-as-modules: registry support for `kind = "theme"`, hot-swap, theme marketplace browse view.

### Phase F — Production hardening
- [ ] Cloudflare Worker + R2 registry (separate repo); CLI switchover.
- [ ] Signed releases (sigstore/cosign or minisign); GitHub Actions provenance.
- [ ] Structured output frames (tables, lists, kv, progress, images).
- [ ] Sandboxing experiments (landlock on Linux).
- [ ] Optional WASM module runtime.

---

## Open questions

- Completion source merging: per-tab module completers + a host-level command palette (`ctrl+p`) that aggregates everything. Confirmed direction.
- Session persistence: tabs reopen on restart, modules opt in to state restoration via `serialize` / `restore`. Confirmed.
- TUI-mode and host input box: in `tui` mode the module owns the entire main area; host input box hidden for that tab. Confirmed.
- IPC perf: stdio is fine for v0. Heavy modules may want a binary frame format later — keep the door open.
- Naming: "hackermode" — keep as final name or treat as codename? Searchability is meh, vibe is good. Decide before public release.
- Telemetry: hard-coded **no**, even opt-in. Reconsider only if community asks.
- Resolver choice: PubGrub via `github.com/mircearoata/pubgrub-go` vs vendoring our own — evaluate during Phase C.
- **Background work policy (TBD in Phase B)**: when a module's tab is unfocused, does it stay running normally (current plan), or is it truly suspended (SIGSTOP) to bound CPU? Leaning: stay running, advisory `focus({focused: false})` RPC. Add a manifest opt-in `background = false` later for heavy modules that should suspend. Implementing both eagerly is over-engineering.
- **Compositor**: stay on lipgloss v1 + the hand-rolled overlay, or migrate to lipgloss v2's cell grid? Decide once we hit the second overlay (dialog or arg-prompt).
- **Command argument types**: stick with string-only args for v0, or design a typed arg system (string / choice / file / number) up front? Leaning string + `choices`; types come later if needed.

---

## Decisions log

- 2026-05-20: Subprocess + JSON-RPC over Go plugins. Standalone Go binaries.
- 2026-05-20: Git-backed registry first, Cloudflare Worker + R2 second.
- 2026-05-20: No bundled modules; `marketplace` is the only built-in tab.
- 2026-05-20: Side panel default width 12%, toggleable with `ctrl+b`.
- 2026-05-20: Tabs are per-session (browser-style), launcher picks the module.
- 2026-05-20: Two module runtime modes — `stream` (stdio text) and `tui` (Bubble Tea over PTY). Control channel on fd 3 is shared by both. In `tui` mode the module owns the entire main area; in `stream` mode the host's input box is used.
- 2026-05-20: Manifest-first install model from v0. `hackermode.toml` + `hackermode.lock` are primary; imperative `install <id>` edits the manifest. PubGrub resolver. Per-platform binary hashes in lockfile. Project / profile / system manifest scopes.
- 2026-05-20: Environments are first-class registry artifacts — published profiles (manifest + optional lock + metadata) installable via `hackermode env install`. Same trust model as modules (no extra capabilities granted to env publishers).
- 2026-05-20: Headless mode (`hackermode run <id>`) — every module is also a regular CLI tool.
- 2026-05-20: Inter-module RPC with `provides` / `consumes` manifest fields, host-mediated.
- 2026-05-20: Command palette (`ctrl+p`, alias `ctrl+k`) lands in Phase A as the host-command surface; modules extend it in Phase B.
- 2026-05-20: Themes are modules with `kind = "theme"`. No special-casing.
- 2026-05-20: Remappable keymap from day one.
- 2026-05-20: No telemetry, ever.
- 2026-05-20: Capability diff on update — prompt when a module update adds new capabilities.
- 2026-05-23: **Host extension points are a fixed, documented contract.** Modules extend the host through a known set of UI hooks (commands, completers, keybinds, widgets, statusline, notifications, themes, settings, launchers) and system hooks (events, inter-module RPC, lifecycle, command hooks). Adding hooks requires a design discussion; modules never reach behind the API.
- 2026-05-23: Commands are namespaced by module ID (`email.compose`). Static (manifest) and dynamic (RPC) registration share one registry, deduped by ID. Host lazy-spawns owning modules on invocation.
- 2026-05-23: Visibility expressions (VS Code-style `when` clauses) gate command/keybind/widget visibility against a small declarative context. Tiny recursive-descent parser; reject unparseable at install time.
- 2026-05-23: Pre/post command hooks are first-class but capability-gated (`hook:commands`). Used by audit / confirmation / dry-run modules.
- 2026-05-23: **Session model**: a tab references a `Session` (module ref, state, scrollback, permissions, context). Introduced in Phase B; pre-Phase-B tabs are flat structs and will be refactored.
- 2026-05-23: **Phase B picks up Phase A's debt explicitly**: real overlay compositor, central dispatch layer, structured JSONL logger, resize debounce, backfill component tests. These are not optional polish — they're prerequisites for module work.
- 2026-05-23: **Unfocused modules keep running** (advisory `focus({focused:false})` RPC). True suspension (SIGSTOP) is a future per-module opt-in, not a host default.
- 2026-05-23: **Every tab has a Session.** A scratch / empty tab is a session with `module: "host"`. Module commands either *transition* the current session to the producing module (same tab, new binding) or *launch* a new tab + session (per-command opt-in in the manifest). Removes "is there a module?" branching across the codebase.
- 2026-05-23: **When-clause grammar is stubbed in Phase B** to `always`, equality, and basic comparisons. Full recursive-descent parser deferred to Phase E. Unparseable expressions are accepted with a logged warning but evaluate to `false` so future grammar simply hides rather than crashes.
