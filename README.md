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

strata init
strata run
```

`strata init` detects Xcode string catalogs, app name, bundle ID, App Store metadata, Swift source roots, and existing catalog languages. Then `strata run` translates missing entries in batches with a terminal progress UI.

Use `strata --help` for overrides like model, dry-run, force, or a custom config path.

## License

MIT
