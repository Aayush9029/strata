# strata

`strata` is a Go CLI that translates the missing entries in Xcode string catalogs (`.xcstrings`) with an LLM on OpenRouter. It uses app context: app name, App Store metadata, and the Swift source near each string. It ships through the Homebrew tap `aayush9029/tap`. Status: active.

## Commands

| Task | Command |
|------|---------|
| Build | `go build -ldflags "-s -w -X main.version=dev" -o strata .` |
| Test | `go test ./...` |
| Set up a project | `strata init` (writes `strata.json`) |
| Translate | `strata run` |
| Translate keys again | `strata run --key "<key>" --force` |
| Release | Load the `oss-cli-release` skill |

## Architecture

| Package | Role |
|---------|------|
| `internal/runner` | `init` wizard, `run` command, flags, `strata.json` config, batches and retries |
| `internal/catalog` | Read and write `.xcstrings`: plurals, substitutions, `stringSet` (App Shortcuts phrases), placeholders |
| `internal/projectinfo` | Finds catalogs, app name, bundle ID, App Store metadata, and languages |
| `internal/sourcecontext` | Finds Swift source context for each key with `ast-grep` |
| `internal/provider` | OpenRouter client (`OPENROUTER_API_KEY`) and model list |
| `internal/ui` | Progress UI |

## Rules

- Write catalogs with `catalog.MarshalXcode` only. It matches the Xcode format: 2-space indent, sorted keys, `" : "` separators, no HTML escaping, and a trailing newline. Other encoders make large diffs.
- Write each catalog after every batch, so a stopped run keeps its progress.
- Check each translation for the same placeholder set and the protected terms (case-sensitive whole words). Retry a failed item two times, then skip it and list it at the end. Do not stop the run.
- Use the plural categories of the target language. Keep substitutions, and translate a substitution entry as one group.
- Honor `shouldTranslate: false`. Keep App Shortcuts tokens such as `${applicationName}` unchanged.
- The default concurrency is 4 batches. `--concurrency` and `"concurrency"` in `strata.json` change it.
- `smart_context` needs `ast-grep`. The tap formula `../../homebrew-tap/Formula/strata.rb` depends on `ast-grep` and builds with `-X main.version=#{version}`.
