<p align="center">
  <img src="assets/icon.png" width="128" alt="strata">
  <h1 align="center">strata</h1>
  <p align="center">Translate missing Xcode string catalog entries with app-aware context</p>
</p>

<p align="center">
  <a href="https://github.com/Aayush9029/strata/releases/latest"><img src="https://img.shields.io/github/v/release/Aayush9029/strata" alt="Release"></a>
  <a href="https://github.com/Aayush9029/strata/blob/main/LICENSE"><img src="https://img.shields.io/github/license/Aayush9029/strata" alt="License"></a>
</p>

## Install

```bash
brew install aayush9029/tap/strata
```

Or tap first:

```bash
brew tap aayush9029/tap
brew install strata
```

## Usage

```bash
export OPENROUTER_API_KEY=...

strata init --app-name Blume --description "food scanner" --terms "Blume,GroceryScan"
strata                                      # use catalogs, languages, and model from strata.json
strata --language es --language ja App.xcstrings
strata --model anthropic/claude-sonnet-4 App.xcstrings
strata --dry-run --discover .               # count missing strings
strata --force --language pt-BR App.xcstrings
strata --name es-MX="Mexican Spanish" App.xcstrings
```

strata refuses to run until a project has `strata.json`. That keeps product names, protected terms, glossary, tone, and source-context settings versioned with the app instead of scattered across one-off command flags.

```json
{
  "app_name": "Blume",
  "description": "food scanner for groceries and ingredient labels",
  "app_context": "Blume is an iOS food scanner.",
  "discover": ["."],
  "languages": ["es", "ja", "pt-BR"],
  "model": "google/gemini-2.5-flash",
  "batch_size": 40,
  "protected_terms": ["GroceryScan", "App Store"],
  "glossary": ["paywall = subscription purchase screen"],
  "style_guide": ["keep onboarding copy warm and concise"],
  "smart_context": true,
  "smart_context_limit": 3,
  "source_roots": ["GroceryScan", "GroceryKit/Sources", "BlumeControls"]
}
```

`strata init` can run as a TUI. It preselects languages already present in your catalogs and suggests popular App Store locales such as `en-GB`, `ar`, `ru`, `hi`, `vi`, `sv`, and `zh-Hant`.

Normal runs always move through translation batches, using `batch_size` to control request size and wait cadence. In a terminal, strata renders a Bubble Tea progress view for the active language, catalog, batch, filled count, and copied non-linguistic strings.

It preserves printf and Xcode substitution placeholders, copies non-linguistic strings without spending a model request, validates protected terms case-insensitively, and reports OpenRouter token/cost usage when the API returns it. When smart context is enabled, strata uses `ast-grep` to send matching Swift file, type, function, and view context with each string so short UI copy is translated with more awareness.

## License

MIT
