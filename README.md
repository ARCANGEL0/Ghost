<div align="center">

<img src="https://raw.githubusercontent.com/swadhinbiswas/Ghost/main/internal/cmd/stats/header.svg" width="600" alt="Ghost CLI Logo">

<p align="center">
  <br>
  <b>👻 A lightning-fast, beautiful AI assistant that lives in your terminal — powered by free models.</b>
  <br>
</p>

[![Go Release](https://img.shields.io/github/actions/workflow/status/swadhinbiswas/Ghost/release.yml?style=flat-square&logo=github&label=Release)](https://github.com/swadhinbiswas/Ghost/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-bd93f9.svg?style=flat-square&logo=opensourceinitiative)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.26-00ADD8?style=flat-square&logo=go)](https://golang.org/)

<br>

<img src="https://raw.githubusercontent.com/swadhinbiswas/Ghost/main/examples/fullscreen.png" width="700" alt="Ghost terminal screenshots">

</div>

---

Hey there! 👋 Welcome to **Ghost** — a terminal-based AI assistant built to be fast, beautiful, and free.

Ghost ships pre-configured with generous **free** AI providers, so you can get world-class answers, code suggestions, and command help without entering a credit card. If you live in the terminal and want instant help without alt-tabbing to a browser, Ghost is for you.

> **Note on UI:** Ghost's interface is built on the foundation of [**Crush**](https://github.com/charmbracelet/crush) by [Charm](https://charm.sh/). Huge thanks to the Charm team for the gorgeous terminal toolkit.

<br>

## ✨ Why Ghost?

- **Free out of the box.** Pre-wired to free providers — no subscription, no API key required to get started.
- **Cross-platform.** A single native Go binary for Linux, macOS, and Windows (amd64 + arm64).
- **Beautiful TUI.** A responsive, themeable terminal interface (Dracula theme included).
- **Connect more gateways.** Add a provider key with `ghost login <provider>` to use its free quota, free models, or credits.

<br>

## 🧠 Models out of the box

These providers can be used without entering an API key:

| Provider | Models | Notes |
| --- | --- | --- |
| **AUTO:FREE** | Remote free routes from Kilo, OI VSCode, and configured OpenRouter | Retries the next route when one fails before responding |
| **Kilo Gateway** | Current `:free` models and `kilo-auto/free` | Free models allow anonymous requests |
| **OI VSCode Server** | Current community endpoint model | Community-hosted availability |
| **Ollama** | Models installed on your machine | Local inference |

Import a Claude Code or Codex conversation into a new Ghost session with `/import --claude <session-id>` or `/import --codex <session-id>`. `/resume` accepts the same flags. See [session import](docs/session-import.md).

Add a key to unlock more:

Use `ghost free` to see current models from OpenRouter, Kilo, Groq, Cerebras, Google AI, Cloudflare, Cohere, NVIDIA NIM, AMD Radeon Cloud, Aion Labs, Hugging Face, LLM7, OpenTyphoon, and Nous Portal. Add a provider key with `ghost login <provider>`.

See [Free model providers](docs/free-model-providers.md) for setup steps, current limits, and data-use terms.

<br>

## 🚀 Getting Started

### The fast way (macOS & Linux)

Detects your OS/architecture and installs the latest release to `/usr/local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/swadhinbiswas/Ghost/main/install.sh | bash
```

### Manual download (Windows, macOS, Linux)

1. Go to the [Releases page](https://github.com/swadhinbiswas/Ghost/releases).
2. Download the archive for your system (`.tar.gz` for macOS/Linux, `.zip` for Windows).
3. Extract it and move the `ghost` binary somewhere on your `PATH`.

### Build from source

Requires **Go 1.26 or newer**:

```bash
git clone https://github.com/swadhinbiswas/Ghost.git
cd Ghost
go build -o ghost ./main.go
sudo mv ghost /usr/local/bin/
```

<br>

## 💻 Usage

Start an interactive chat session:

```bash
ghost
```

Run a one-off prompt without entering the full UI:

```bash
ghost run "Write a python script to parse a CSV file"
```

Other handy commands:

```bash
ghost free                 # list free and quota-based model catalogs
ghost models               # list available models
ghost login                # add credentials for keyed providers
ghost theme                # switch the TUI theme
ghost update-providers     # refresh the provider registry
ghost session              # manage saved sessions
ghost --help               # see everything Ghost can do
```

<br>

## 🛠️ Configuration

Ghost reads JSON config from standard locations (`$XDG_CONFIG_HOME/ghost/ghost.json` or `~/.config/ghost/ghost.json`). A few useful environment variables:

| Variable | Purpose |
| --- | --- |
| `GHOST_DISABLE_PROVIDER_AUTO_UPDATE` | Disable automatic provider/model refresh |
| `GHOST_DISABLE_DEFAULT_PROVIDERS` | Skip all built-in providers (BYO only) |
| `CLOUDFLARE_ACCOUNT_ID` | Cloudflare Workers AI account used for model discovery and inference |

Ghost refreshes available live model catalogs at startup and uses its cache when a provider is offline. Run `ghost free` to browse the latest models available to the configured accounts.

<br>

## 🙏 Acknowledgments

- **UI foundation:** [Crush](https://github.com/charmbracelet/crush) and the [Charm](https://charm.sh/) ecosystem (`lipgloss`, `bubbletea`, `catwalk`).
- **Free providers:** Kilo, the OI VSCode community, and the providers listed in the [gateway guide](docs/free-model-providers.md).

<br>

## 📄 License

Ghost is open-source software licensed under the [MIT License](LICENSE). Fork it, mod it, make it your own.

<br>

## 📸 Screenshots

<p align="center">
  <img src="https://raw.githubusercontent.com/swadhinbiswas/Ghost/main/examples/ghostcode.png" width="380" alt="Ghost code assistance">&nbsp;
  <img src="https://raw.githubusercontent.com/swadhinbiswas/Ghost/main/examples/theme.png" width="380" alt="Ghost theme selector">&nbsp;
  <img src="https://raw.githubusercontent.com/swadhinbiswas/Ghost/main/examples/commands.png" width="380" alt="Ghost commands">&nbsp;
  <img src="https://raw.githubusercontent.com/swadhinbiswas/Ghost/main/examples/image1.png" width="380" alt="Ghost interface">
</p>
