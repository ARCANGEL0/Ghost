# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `/import --claude <session-id>` and `/import --codex <session-id>` to bring coding-agent transcripts into a new Ghost session.
- `ghost login codex` to sign in through the Codex CLI device-code flow.
- `AUTO:FREE`, which tries remote Kilo and OI VSCode routes, plus configured OpenRouter free routes.
- Live model catalogs for OpenRouter, Kilo, NVIDIA NIM, Groq,
  Cerebras, Google AI, Cloudflare, Cohere, AMD Radeon Cloud, Aion Labs, Hugging
  Face, LLM7, OpenTyphoon, Nous Portal, and local Ollama, with cached fallback
  when a refresh fails.
- `ghost login <provider>` setup for API-key gateways and `ghost free` live model
  discovery.
- Astro-based website and documentation under `site/` (built with Bun).
- Continuous Integration: build, vet, test (with race detector and coverage),
  and `golangci-lint` on every push and pull request.
- GitHub Pages deployment workflow for the website.
- Project governance files: `CONTRIBUTING.md`, `SECURITY.md`,
  `CODE_OF_CONDUCT.md`, issue/PR templates, and `.editorconfig`.
- `LICENSE` (MIT).

### Changed
- Rewrote `README.md` to accurately describe shipped providers and models.
- Standardized the GitHub repository reference to `swadhinbiswas/Ghost`.

### Fixed
- Keep Kilo Auto Free in the model picker when its live catalog is unavailable.
- Remove OpenCode-only free models from Ghost's hosted gateway list.
- Repository hygiene: added `.gitignore` and removed committed build artifacts,
  tooling archives, and throwaway scripts.

<!--
Release sections below are populated by GoReleaser on tagged releases.
Example:

## [1.0.0] - 2026-06-11
### Added
- Initial public release.
-->
