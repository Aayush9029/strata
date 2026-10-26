package runner

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/Aayush9029/strata/internal/catalog"
	"github.com/Aayush9029/strata/internal/projectinfo"
	"github.com/Aayush9029/strata/internal/provider"
	"github.com/Aayush9029/strata/internal/sourcecontext"
	"github.com/Aayush9029/strata/internal/ui"
)

var LanguageNames = map[string]string{
	"ar":      "Arabic",
	"de":      "German",
	"en":      "English",
	"es":      "Spanish",
	"es-419":  "Latin American Spanish",
	"es-MX":   "Mexican Spanish",
	"es-US":   "United States Spanish",
	"fr":      "French",
	"it":      "Italian",
	"ja":      "Japanese",
	"ko":      "Korean",
	"nl":      "Dutch",
	"pt-BR":   "Brazilian Portuguese",
	"pt-PT":   "European Portuguese",
	"zh-Hans": "Simplified Chinese",
	"zh-Hant": "Traditional Chinese",
}

const defaultConcurrency = 4

type Config struct {
	CatalogPaths  []string
	Languages     []string
	Names         map[string]string
	Model         string
	BatchSize     int
	Concurrency   int
	Force         bool
	DryRun        bool
	ProviderName  string
	Timeout       time.Duration
	DiscoverRoots []string
	ConfigPath    string
	Project       provider.ProjectConfig
}

type Result struct {
	Catalogs   int
	Languages  int
	Candidates int
	Translated int
	Planned    int
	Copied     int
	Batches    int
	Skipped    []Skip
	Usage      provider.Usage
	Duration   time.Duration
}

func Run(ctx context.Context, args []string, version string, out, stderr io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "init":
			return runInit(args[1:], out, stderr)
		case "run", "translate":
			return runTranslate(ctx, args[1:], out, stderr)
		case "--version", "-v", "version":
			fmt.Fprintf(out, "strata %s\n", version)
			return nil
		case "--help", "-h", "help":
			printHelp(out)
			return nil
		}
	}
	printHelp(out)
	return nil
}

func runTranslate(ctx context.Context, args []string, out, stderr io.Writer) error {
	config, err := parseArgs(args)
	if err != nil {
		return err
	}

	printer := ui.New(out, stderr)
	if config.ConfigPath == "" {
		printer.Warning("strata is not configured")
		printer.Dim("Run `strata init` first, then run `strata`.")
		fmt.Fprintln(out)
		printHelp(out)
		return errors.New("missing strata.json")
	}
	fileProject, err := loadProjectConfig(config.ConfigPath)
	if err != nil {
		return err
	}
	config.Project = mergeProjectConfig(fileProject, config.Project)
	applyProjectDefaults(&config)

	for _, root := range config.DiscoverRoots {
		paths, err := catalog.DiscoverCatalogs(root)
		if err != nil {
			return err
		}
		config.CatalogPaths = append(config.CatalogPaths, paths...)
	}
	if len(config.CatalogPaths) == 0 {
		return errors.New("no .xcstrings files configured; run `strata init` or add catalogs/discover to strata.json")
	}

	translator, err := provider.FromEnvironment(config.ProviderName, config.Model, config.Timeout)
	if err != nil {
		return err
	}
	result, err := Localize(ctx, config, translator, printer)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			printSummary(printer, config, result, true)
			return nil
		}
		printSkipped(printer, result.Skipped)
		return err
	}
	printSummary(printer, config, result, false)
	return nil
}

func printSkipped(printer ui.Printer, skipped []Skip) {
	if len(skipped) == 0 {
		return
	}
	printer.Warning("skipped %d strings that failed validation after retries:", len(skipped))
	for _, skip := range skipped {
		key := skip.Key
		if skip.Variant != "" {
			key += " (" + skip.Variant + ")"
		}
		printer.Warning("  %s [%s] %q: %s", ui.ShortPath(skip.Catalog), skip.Language, key, skip.Reason)
	}
}

func printSummary(printer ui.Printer, config Config, result Result, canceled bool) {
	if canceled {
		printer.Warning("stopped after %s", result.Duration.Round(time.Millisecond))
	} else if config.DryRun {
		printer.Success("would fill %d strings across %d catalogs and %d languages in %s", result.Planned, result.Catalogs, result.Languages, result.Duration.Round(time.Millisecond))
	} else {
		printer.Success("translated %d/%d strings across %d catalogs and %d languages in %s", result.Translated, result.Candidates, result.Catalogs, result.Languages, result.Duration.Round(time.Millisecond))
	}
	if canceled {
		if config.DryRun {
			printer.Dim("Partial plan: %d/%d strings across %d batches", result.Planned, result.Candidates, result.Batches)
		} else {
			printer.Dim("Partial result: %d/%d strings translated, %d copied, %d batches completed", result.Translated, result.Candidates, result.Copied, result.Batches)
		}
	}
	if config.DryRun {
		return
	}
	printSkipped(printer, result.Skipped)
	if result.Usage.TotalTokens > 0 || result.Usage.HasCost {
		if result.Usage.HasCost {
			printer.Dim("OpenRouter usage: %d prompt + %d completion = %d tokens, %.6f credits", result.Usage.PromptTokens, result.Usage.CompletionTokens, result.Usage.TotalTokens, result.Usage.Cost)
		} else {
			printer.Dim("OpenRouter usage: %d prompt + %d completion = %d tokens", result.Usage.PromptTokens, result.Usage.CompletionTokens, result.Usage.TotalTokens)
		}
	}
}

func Localize(ctx context.Context, config Config, translator provider.Translator, printer ui.Printer) (Result, error) {
	started := time.Now()
	catalogs, err := catalog.LoadMany(config.CatalogPaths)
	if err != nil {
		return Result{}, err
	}
	languages := catalog.ConfiguredLanguages(catalogs, config.Languages)
	if len(languages) == 0 {
		return Result{}, errors.New("no target languages found; add localizations to a catalog or pass --language")
	}
	var sourceIndex *sourcecontext.Index
	result := Result{Catalogs: len(catalogs), Languages: len(languages)}
	progress, runCtx := ui.NewProgress(ctx, printer.Output(), printer.IsTerminal())
	defer progress.Close()
	for languageIndex, language := range languages {
		if err := runCtx.Err(); err != nil {
			result.Duration = time.Since(started)
			return result, err
		}
		displayName := languageName(language, config.Names)
		if printer.IsTerminal() {
			progress.Send(ui.ProgressEvent{
				Phase:         "language",
				Language:      language,
				LanguageName:  displayName,
				LanguageIndex: languageIndex + 1,
				LanguageTotal: len(languages),
				CatalogTotal:  len(catalogs),
				Message:       "starting language",
			})
		} else {
			printer.Header("%s (%s)", language, displayName)
		}
		for catalogIndex, cat := range catalogs {
			coverage := cat.Coverage(language)
			items := cat.MissingItems(language, config.Force)
			if len(items) > 0 && config.Project.SmartContext {
				if sourceIndex == nil {
					index, err := sourcecontext.Build(config.Project.SourceRoots)
					if err != nil {
						return Result{}, err
					}
					sourceIndex = index
				}
				limit := config.Project.SmartContextLimit
				if limit <= 0 {
					limit = 3
				}
				items = sourceIndex.Attach(items, limit)
			}
			result.Candidates += len(items)
			if len(items) == 0 {
				if printer.IsTerminal() {
					progress.Send(ui.ProgressEvent{
						Phase:         "catalog",
						Language:      language,
						LanguageName:  displayName,
						LanguageIndex: languageIndex + 1,
						LanguageTotal: len(languages),
						Catalog:       cat.Path,
						CatalogIndex:  catalogIndex + 1,
						CatalogTotal:  len(catalogs),
						Existing:      coverage.Translated,
						Total:         coverage.Total,
						Message:       "up to date",
					})
				} else {
					printer.Dim("  %s: up to date", cat.Path)
				}
				continue
			}
			event := ui.ProgressEvent{
				Phase:         "catalog",
				Language:      language,
				LanguageName:  displayName,
				LanguageIndex: languageIndex + 1,
				LanguageTotal: len(languages),
				Catalog:       cat.Path,
				CatalogIndex:  catalogIndex + 1,
				CatalogTotal:  len(catalogs),
				Candidates:    len(items),
				Existing:      coverage.Translated,
				Total:         coverage.Total,
			}
			outcome, err := localizeCatalog(runCtx, cat, language, displayName, items, config, translator, printer, progress, event)
			filled := outcome.Translated + outcome.Copied
			result.Planned += filled
			if !config.DryRun {
				result.Translated += filled
			}
			result.Copied += outcome.Copied
			result.Batches += outcome.Batches
			result.Usage = result.Usage.Add(outcome.Usage)
			result.Skipped = append(result.Skipped, outcome.Skipped...)
			if !config.DryRun && filled > 0 {
				if writeErr := cat.Write(); writeErr != nil {
					return result, writeErr
				}
			}
			if err != nil {
				result.Duration = time.Since(started)
				return result, err
			}
			if printer.IsTerminal() {
				event.Phase = "catalog"
				event.Filled = filled
				event.Copied = outcome.Copied
				if config.DryRun {
					event.Message = "dry run complete"
				} else {
					event.Message = "catalog complete"
				}
				progress.Send(event)
			} else if config.DryRun {
				printer.Status("%s: would fill %d/%d", cat.Path, filled, len(items))
			} else {
				printer.Status("%s: %d/%d filled", cat.Path, filled, len(items))
			}
		}
	}
	result.Duration = time.Since(started)
	return result, nil
}

type catalogOutcome struct {
	Translated int
	Copied     int
	Batches    int
	Usage      provider.Usage
	Skipped    []Skip
}

// localizeCatalog translates items for one catalog and language. Batches run
// on a worker pool; results are applied and written under one lock, so a
// stopped run keeps every completed batch.
func localizeCatalog(ctx context.Context, cat *catalog.Catalog, language, displayName string, items []catalog.Item, config Config, translator provider.Translator, printer ui.Printer, progress *ui.Progress, event ui.ProgressEvent) (catalogOutcome, error) {
	outcome := catalogOutcome{}
	toTranslate := []catalog.Item{}
	for _, item := range items {
		if !catalog.HasLinguisticText(item.Source) {
			if !config.DryRun {
				cat.ApplyTranslation(language, item.Key, item.VariantPath, item.Source)
			}
			outcome.Copied++
			continue
		}
		toTranslate = append(toTranslate, item)
	}
	batches := splitBatches(toTranslate, config.BatchSize)
	event.Copied = outcome.Copied
	event.Filled = outcome.Copied
	event.BatchTotal = len(batches)
	event.Message = "prepared batches"
	progress.Send(event)

	if config.DryRun {
		for _, batch := range batches {
			outcome.Batches++
			outcome.Translated += len(batch)
			if !printer.IsTerminal() {
				printer.Dim("  %s: would translate %d strings", cat.Path, len(batch))
			}
			event.BatchIndex = outcome.Batches
			event.Filled = outcome.Translated + outcome.Copied
			event.Message = "batch planned"
			progress.Send(event)
		}
		return outcome, nil
	}

	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	var firstErr error
	fail := func(err error) {
		if firstErr == nil {
			firstErr = err
			cancel()
		}
	}
	jobs := make(chan []catalog.Item)
	var wg sync.WaitGroup
	for range max(1, min(config.Concurrency, len(batches))) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range jobs {
				result, err := translateBatch(workCtx, translator, provider.Request{
					LanguageCode: language,
					LanguageName: displayName,
					Items:        batch,
					Project:      config.Project,
				}, cat.Path)
				mu.Lock()
				outcome.Usage = outcome.Usage.Add(result.Usage)
				if err != nil {
					fail(err)
					mu.Unlock()
					continue
				}
				for _, translation := range result.Translations {
					cat.ApplyTranslation(language, translation.Item.Key, translation.Item.VariantPath, translation.Text)
				}
				outcome.Translated += len(result.Translations)
				outcome.Skipped = append(outcome.Skipped, result.Skipped...)
				outcome.Batches++
				if err := cat.Write(); err != nil {
					fail(err)
				}
				event.BatchIndex = outcome.Batches
				event.Filled = outcome.Translated + outcome.Copied
				event.Message = "batch complete"
				progress.Send(event)
				mu.Unlock()
			}
		}()
	}
feed:
	for _, batch := range batches {
		select {
		case jobs <- batch:
		case <-workCtx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return outcome, firstErr
	}
	return outcome, ctx.Err()
}

func splitBatches(items []catalog.Item, size int) [][]catalog.Item {
	var batches [][]catalog.Item
	for start := 0; start < len(items); start += size {
		batches = append(batches, items[start:min(start+size, len(items))])
	}
	return batches
}

// Skip records a string that still failed validation after all retries.
type Skip struct {
	Catalog  string
	Language string
	Key      string
	Variant  string
	Reason   string
}

type acceptedTranslation struct {
	Item catalog.Item
	Text string
}

type batchResult struct {
	Translations []acceptedTranslation
	Skipped      []Skip
	Usage        provider.Usage
}

// validationRetries is the number of follow-up requests for items that fail
// validation. Each follow-up sends only the failed items with the reason.
const validationRetries = 2

func translateBatch(ctx context.Context, translator provider.Translator, request provider.Request, catalogPath string) (batchResult, error) {
	result := batchResult{}
	pending := request.Items
	terms := request.Project.AllProtectedTerms()
	for attempt := 0; attempt <= validationRetries && len(pending) > 0; attempt++ {
		request.Items = pending
		response, err := translateWithRetry(ctx, translator, request)
		if err != nil {
			return result, err
		}
		result.Usage = result.Usage.Add(response.Usage)
		var failed []catalog.Item
		for _, item := range pending {
			text, ok := response.Translations[item.ID]
			reason := validateItem(item, text, ok, terms)
			if reason == "" {
				result.Translations = append(result.Translations, acceptedTranslation{Item: item, Text: text})
				continue
			}
			item.PreviousTranslation = text
			item.PreviousError = reason
			failed = append(failed, item)
		}
		pending = failed
	}
	for _, item := range pending {
		result.Skipped = append(result.Skipped, Skip{
			Catalog:  catalogPath,
			Language: request.LanguageCode,
			Key:      item.Key,
			Variant:  strings.Join(item.VariantPath, "."),
			Reason:   item.PreviousError,
		})
	}
	return result, nil
}

func validateItem(item catalog.Item, text string, ok bool, terms []string) string {
	if !ok || strings.TrimSpace(text) == "" {
		return "missing translation"
	}
	if err := catalog.ValidateTranslation(item.Source, text); err != nil {
		return err.Error()
	}
	if err := validateProtectedTerms(item.Source, text, terms); err != nil {
		return err.Error()
	}
	return ""
}

func translateWithRetry(ctx context.Context, translator provider.Translator, request provider.Request) (provider.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		result, err := translator.Translate(ctx, request)
		if err == nil {
			return result, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return provider.Response{}, ctx.Err()
		case <-time.After(time.Duration(1<<attempt) * time.Second):
		}
	}
	return provider.Response{}, lastErr
}

func parseArgs(args []string) (Config, error) {
	flags := flag.NewFlagSet("strata", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var languages multiFlag
	var names multiFlag
	var discovers multiFlag
	config := Config{
		Names:        map[string]string{},
		Model:        envDefault("OPENROUTER_MODEL", "google/gemini-2.5-flash"),
		BatchSize:    40,
		Concurrency:  defaultConcurrency,
		ProviderName: "openrouter",
		Timeout:      120 * time.Second,
	}
	flags.Var(&languages, "language", "BCP-47 language code. Repeat for multiple languages.")
	flags.Var(&names, "name", "Language display override as code=name.")
	flags.StringVar(&config.Model, "model", config.Model, "OpenRouter model name.")
	flags.IntVar(&config.BatchSize, "batch-size", config.BatchSize, "Strings per translation request.")
	flags.IntVar(&config.Concurrency, "concurrency", config.Concurrency, "Translation requests to run in parallel.")
	flags.BoolVar(&config.Force, "force", false, "Translate even when a target localization already exists.")
	flags.BoolVar(&config.DryRun, "dry-run", false, "Report work without writing files or calling the provider.")
	flags.StringVar(&config.ProviderName, "provider", config.ProviderName, "Translation provider: openrouter or mock.")
	flags.Var(&discovers, "discover", "Recursively find .xcstrings files under this path. Repeat for multiple roots.")
	flags.StringVar(&config.ConfigPath, "config", "", "JSON project config. Defaults to strata.json when present.")
	timeoutSeconds := flags.Int("timeout", int(config.Timeout.Seconds()), "Provider request timeout in seconds.")

	if err := flags.Parse(args); err != nil {
		return config, err
	}
	if config.BatchSize <= 0 {
		return config, errors.New("--batch-size must be greater than zero")
	}
	if config.Concurrency <= 0 {
		return config, errors.New("--concurrency must be greater than zero")
	}
	config.Timeout = time.Duration(*timeoutSeconds) * time.Second
	config.Languages = languages
	config.DiscoverRoots = discovers
	if config.ConfigPath == "" {
		if _, err := os.Stat("strata.json"); err == nil {
			config.ConfigPath = "strata.json"
		}
	}
	for _, rawName := range names {
		code, name, ok := strings.Cut(rawName, "=")
		if !ok || strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" {
			return config, errors.New("--name must use code=name, for example --name es=Spanish")
		}
		config.Names[code] = name
	}
	for _, arg := range flags.Args() {
		if strings.HasSuffix(arg, ".xcstrings") {
			config.CatalogPaths = append(config.CatalogPaths, arg)
			continue
		}
		matches, err := filepath.Glob(filepath.Join(arg, "**", "*.xcstrings"))
		if err == nil && len(matches) > 0 {
			config.CatalogPaths = append(config.CatalogPaths, matches...)
			continue
		}
		config.CatalogPaths = append(config.CatalogPaths, arg)
	}
	return config, nil
}

func runInit(args []string, out, stderr io.Writer) error {
	flags := flag.NewFlagSet("strata init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var terms string
	var glossary string
	var style string
	var sourceRoots string
	var catalogs string
	var discover string
	var languages string
	var model string
	var path string
	var force bool
	config := provider.ProjectConfig{SmartContextLimit: 3}
	flags.StringVar(&path, "config", "strata.json", "Config file to write.")
	flags.StringVar(&config.AppName, "app-name", "", "App or product name.")
	flags.StringVar(&config.Description, "description", "", "Short app description.")
	flags.StringVar(&model, "model", "", "OpenRouter model to write into config.")
	flags.StringVar(&terms, "terms", "", "Comma-separated protected terms.")
	flags.StringVar(&style, "style", "", "Comma-separated style rules.")
	flags.StringVar(&catalogs, "catalogs", "", "Comma-separated .xcstrings files.")
	flags.StringVar(&discover, "discover", "", "Comma-separated roots to discover .xcstrings files.")
	flags.StringVar(&languages, "languages", "", "Comma-separated BCP-47 languages.")
	flags.BoolVar(&force, "force", false, "Replace an existing config instead of merging with it.")
	flags.BoolVar(&config.SmartContext, "smart-context", true, "Include Swift file/type/function context in translation prompts.")
	flags.IntVar(&config.SmartContextLimit, "smart-context-limit", config.SmartContextLimit, "Max source occurrences per string.")
	if err := flags.Parse(args); err != nil {
		return err
	}
	printer := ui.New(out, stderr)
	inferred := projectinfo.Infer(".")
	if isTerminalReader(os.Stdin) && shouldRunInitWizard(args) {
		if err := runInitForm(&config, &model, &terms, &style, inferred); err != nil {
			return err
		}
	} else {
		applyInitInference(&config, &terms, &catalogs, &discover, &sourceRoots, inferred)
	}
	applyInitInference(&config, &terms, &catalogs, &discover, &sourceRoots, inferred)
	config.ProtectedTerms = splitCSV(terms)
	if strings.TrimSpace(model) != "" {
		config.Model = strings.TrimSpace(model)
	}
	config.Glossary = splitCSV(glossary)
	config.StyleGuide = splitCSV(style)
	config.SourceRoots = splitCSV(sourceRoots)
	config.Catalogs = splitCSV(catalogs)
	config.Discover = splitCSV(discover)
	config.Languages = splitCSV(languages)
	if isTerminalReader(os.Stdin) && len(config.Languages) == 0 {
		selected, err := chooseInitLanguages(config)
		if err != nil {
			return err
		}
		config.Languages = selected
	}
	if config.AppName == "" {
		return errors.New("app name is required; pass --app-name")
	}
	if !force {
		existing, err := loadProjectConfig(path)
		if err == nil {
			config = mergeProjectConfig(existing, config)
			config = dedupeProjectConfig(config)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}
	printer.Success("wrote %s", path)
	return nil
}

func shouldRunInitWizard(args []string) bool {
	for _, arg := range args {
		name := strings.TrimPrefix(arg, "--")
		name, _, _ = strings.Cut(name, "=")
		switch name {
		case "app-name", "description", "model", "terms", "style", "catalogs", "discover", "languages", "smart-context", "smart-context-limit", "force":
			return false
		}
	}
	return true
}

func loadProjectConfig(path string) (provider.ProjectConfig, error) {
	if strings.TrimSpace(path) == "" {
		return provider.ProjectConfig{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return provider.ProjectConfig{}, err
	}
	var config provider.ProjectConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return provider.ProjectConfig{}, fmt.Errorf("%s: decode config JSON: %w", path, err)
	}
	return config, nil
}

func mergeProjectConfig(base, override provider.ProjectConfig) provider.ProjectConfig {
	if strings.TrimSpace(override.AppName) != "" {
		base.AppName = override.AppName
	}
	if strings.TrimSpace(override.Description) != "" {
		base.Description = override.Description
	}
	base.AppContext = joinNonEmpty(base.AppContext, override.AppContext)
	base.ProtectedTerms = append(base.ProtectedTerms, override.ProtectedTerms...)
	base.Glossary = append(base.Glossary, override.Glossary...)
	base.StyleGuide = append(base.StyleGuide, override.StyleGuide...)
	base.AdditionalGuidance = joinNonEmpty(base.AdditionalGuidance, override.AdditionalGuidance)
	if len(override.Catalogs) > 0 {
		base.Catalogs = append(base.Catalogs, override.Catalogs...)
	}
	if len(override.Discover) > 0 {
		base.Discover = append(base.Discover, override.Discover...)
	}
	if len(override.Languages) > 0 {
		base.Languages = append(base.Languages, override.Languages...)
	}
	if len(override.LanguageNames) > 0 {
		if base.LanguageNames == nil {
			base.LanguageNames = map[string]string{}
		}
		for code, name := range override.LanguageNames {
			base.LanguageNames[code] = name
		}
	}
	if override.Model != "" {
		base.Model = override.Model
	}
	if override.BatchSize > 0 {
		base.BatchSize = override.BatchSize
	}
	if override.Concurrency > 0 {
		base.Concurrency = override.Concurrency
	}
	if override.BundleID != "" {
		base.BundleID = override.BundleID
	}
	if override.AppStoreID != "" {
		base.AppStoreID = override.AppStoreID
	}
	if override.SmartContext {
		base.SmartContext = true
	}
	if override.SmartContextLimit > 0 {
		base.SmartContextLimit = override.SmartContextLimit
	}
	if len(override.SourceRoots) > 0 {
		base.SourceRoots = append(base.SourceRoots, override.SourceRoots...)
	}
	return base
}

func dedupeProjectConfig(config provider.ProjectConfig) provider.ProjectConfig {
	config.Catalogs = uniqueInOrder(config.Catalogs)
	config.Discover = uniqueInOrder(config.Discover)
	config.Languages = uniqueInOrder(config.Languages)
	config.ProtectedTerms = uniqueInOrder(config.ProtectedTerms)
	config.Glossary = uniqueInOrder(config.Glossary)
	config.StyleGuide = uniqueInOrder(config.StyleGuide)
	config.SourceRoots = uniqueInOrder(config.SourceRoots)
	return config
}

func applyProjectDefaults(config *Config) {
	if len(config.CatalogPaths) == 0 {
		config.CatalogPaths = append(config.CatalogPaths, config.Project.Catalogs...)
	}
	if len(config.DiscoverRoots) == 0 {
		config.DiscoverRoots = append(config.DiscoverRoots, config.Project.Discover...)
	}
	if len(config.Languages) == 0 {
		config.Languages = append(config.Languages, config.Project.Languages...)
	}
	if config.Names == nil {
		config.Names = map[string]string{}
	}
	for code, name := range config.Project.LanguageNames {
		if config.Names[code] == "" {
			config.Names[code] = name
		}
	}
	defaultModel := envDefault("OPENROUTER_MODEL", "google/gemini-2.5-flash")
	if config.Project.Model != "" && config.Model == defaultModel {
		config.Model = config.Project.Model
	}
	if config.Project.BatchSize > 0 && config.BatchSize == 40 {
		config.BatchSize = config.Project.BatchSize
	}
	if config.Project.Concurrency > 0 && config.Concurrency == defaultConcurrency {
		config.Concurrency = config.Project.Concurrency
	}
}

func joinNonEmpty(values ...string) string {
	parts := []string{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			parts = append(parts, strings.TrimSpace(value))
		}
	}
	return strings.Join(parts, "\n\n")
}

func validateProtectedTerms(source, translation string, terms []string) error {
	for _, term := range terms {
		if containsWord(source, term) && !containsWord(translation, term) {
			return fmt.Errorf("protected term %q was changed or removed", term)
		}
	}
	return nil
}

// containsWord reports whether text contains term as a whole word or phrase,
// case-sensitively. A word boundary is any rune that is not a letter or digit.
// Han, kana, Hangul, and Thai do not count as letters here, because those
// scripts write words without spaces ("Plexで" contains "Plex").
func containsWord(text, term string) bool {
	if term == "" {
		return false
	}
	for offset := 0; offset <= len(text)-len(term); {
		index := strings.Index(text[offset:], term)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(term)
		before, _ := utf8.DecodeLastRuneInString(text[:start])
		after, _ := utf8.DecodeRuneInString(text[end:])
		first, _ := utf8.DecodeRuneInString(term)
		last, _ := utf8.DecodeLastRuneInString(term)
		if (start == 0 || !isWordRune(first) || !isWordRune(before)) && (end == len(text) || !isWordRune(last) || !isWordRune(after)) {
			return true
		}
		_, size := utf8.DecodeRuneInString(text[start:])
		offset = start + size
	}
	return false
}

func isWordRune(char rune) bool {
	if unicode.In(char, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Thai) {
		return false
	}
	return unicode.IsLetter(char) || unicode.IsDigit(char)
}

func splitCSV(value string) []string {
	var values []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func isTerminalReader(file *os.File) bool {
	stat, err := file.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func applyInitInference(config *provider.ProjectConfig, terms, catalogs, discover, sourceRoots *string, inferred projectinfo.Info) {
	if config.AppName == "" {
		config.AppName = inferred.AppName
	}
	if config.Description == "" {
		config.Description = inferred.Description
	}
	if config.BundleID == "" {
		config.BundleID = inferred.BundleID
	}
	if config.AppStoreID == "" {
		config.AppStoreID = inferred.AppStoreID
	}
	if strings.TrimSpace(*terms) == "" {
		*terms = strings.Join(inferred.Terms, ",")
	}
	if strings.TrimSpace(*catalogs) == "" && len(inferred.Catalogs) > 0 {
		*catalogs = strings.Join(inferred.Catalogs, ",")
	}
	if strings.TrimSpace(*discover) == "" {
		if len(inferred.Discover) > 0 {
			*discover = strings.Join(inferred.Discover, ",")
		} else {
			*discover = "."
		}
	}
	if strings.TrimSpace(*sourceRoots) == "" {
		if len(inferred.SourceRoots) > 0 {
			*sourceRoots = strings.Join(inferred.SourceRoots, ",")
		} else {
			*sourceRoots = "."
		}
	}
}

func runInitForm(config *provider.ProjectConfig, model, terms, style *string, inferred projectinfo.Info) error {
	modelChoices := latestModelOptions(config.Model)
	modelChoice := defaultModelChoice(config.Model, modelChoices)
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("strata init").
				Description(initSummary(inferred)),
			huh.NewInput().
				Title("App name").
				Description("Press Return to accept the inferred value, or edit it.").
				Placeholder(inferred.AppName).
				Value(&config.AppName),
			huh.NewInput().
				Title("Description").
				Description("Press Return to use the App Store description, or write a shorter prompt hint.").
				Placeholder(inferred.Description).
				Value(&config.Description),
			huh.NewSelect[string]().
				Title("Latest models").
				Description("Fetched from OpenRouter live models.").
				Options(modelOptions(modelChoice, modelChoices)...).
				Value(&modelChoice),
			huh.NewInput().
				Title("Protected terms").
				Description("Comma-separated terms that must not be translated.").
				Placeholder(strings.Join(inferred.Terms, ",")).
				Value(terms),
			huh.NewInput().
				Title("Style guide").
				Description("Optional comma-separated tone or copy rules.").
				Placeholder("clear, concise, native iOS copy").
				Value(style),
			huh.NewConfirm().
				Title("Enable smart context?").
				Description("Uses inferred Swift source roots to add file/type/function/view context.").
				Value(&config.SmartContext),
		),
	).WithTheme(initTheme()).WithWidth(88).Run(); err != nil {
		return err
	}
	*model = modelChoice
	return nil
}

func initTheme() *huh.Theme {
	theme := huh.ThemeCharm()
	theme.Focused.Base = theme.Focused.Base.BorderStyle(lipgloss.HiddenBorder()).PaddingLeft(0)
	theme.Blurred.Base = theme.Blurred.Base.BorderStyle(lipgloss.HiddenBorder()).PaddingLeft(0)
	theme.Focused.SelectSelector = lipgloss.NewStyle().Foreground(lipgloss.Color("35")).SetString("› ")
	theme.Focused.MultiSelectSelector = lipgloss.NewStyle().Foreground(lipgloss.Color("35")).SetString("› ")
	theme.Focused.TextInput.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("35")).SetString("› ")
	theme.Focused.Title = theme.Focused.Title.Foreground(lipgloss.Color("39")).Bold(true)
	theme.Focused.NoteTitle = theme.Focused.NoteTitle.Foreground(lipgloss.Color("39")).Bold(true)
	theme.Focused.Description = theme.Focused.Description.Foreground(lipgloss.Color("244"))
	theme.Focused.SelectedPrefix = lipgloss.NewStyle().Foreground(lipgloss.Color("35")).SetString("✓ ")
	theme.Focused.UnselectedPrefix = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).SetString("  ")
	return theme
}

func initSummary(inferred projectinfo.Info) string {
	lines := []string{"Found project defaults. Press Return on each field to accept its suggested value."}
	if inferred.BundleID != "" {
		lines = append(lines, "Bundle ID: "+inferred.BundleID)
	}
	if inferred.AppStoreID != "" {
		lines = append(lines, "App Store ID: "+inferred.AppStoreID)
	}
	if len(inferred.Catalogs) > 0 {
		lines = append(lines, fmt.Sprintf("String catalogs: %d", len(inferred.Catalogs)))
	}
	if len(inferred.SourceRoots) > 0 {
		lines = append(lines, "Swift roots: "+strings.Join(inferred.SourceRoots, ", "))
	}
	return strings.Join(lines, "\n")
}

type modelChoice struct {
	Label string
	Value string
	Score float64
}

type openRouterModelsResponse struct {
	Data []openRouterModel `json:"data"`
}

type openRouterModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Created       int64  `json:"created"`
	Description   string `json:"description"`
	ContextLength int    `json:"context_length"`
	Architecture  struct {
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
	Pricing struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing"`
	SupportedParameters []string `json:"supported_parameters"`
}

func defaultModelChoice(configured string, choices []modelChoice) string {
	if configured == "" {
		configured = envDefault("OPENROUTER_MODEL", "")
	}
	for _, option := range choices {
		if option.Value == configured {
			return configured
		}
	}
	if configured != "" {
		return choices[0].Value
	}
	if len(choices) > 0 {
		return choices[0].Value
	}
	return "openrouter/auto"
}

func modelOptions(selected string, choices []modelChoice) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(choices))
	for _, option := range choices {
		options = append(options, huh.NewOption(option.Label, option.Value).Selected(option.Value == selected))
	}
	return options
}

func latestModelOptions(configured string) []modelChoice {
	choices, err := fetchLatestOpenRouterModels()
	if err != nil || len(choices) == 0 {
		choices = fallbackModelOptions()
	}
	if configured != "" && !containsModel(choices, configured) {
		choices = append([]modelChoice{{Label: "Current config: " + configured, Value: configured, Score: 999}}, choices...)
	}
	if len(choices) > 20 {
		choices = choices[:20]
	}
	return choices
}

func fetchLatestOpenRouterModels() ([]modelChoice, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get("https://openrouter.ai/api/v1/models")
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenRouter returned %s", response.Status)
	}
	var payload openRouterModelsResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	var choices []modelChoice
	for _, model := range payload.Data {
		if !isUsefulTranslationModel(model) {
			continue
		}
		score := scoreOpenRouterModel(model)
		choices = append(choices, modelChoice{
			Label: modelLabel(model, score),
			Value: model.ID,
			Score: score,
		})
	}
	slices.SortFunc(choices, func(a, b modelChoice) int {
		if a.Score > b.Score {
			return -1
		}
		if a.Score < b.Score {
			return 1
		}
		return strings.Compare(a.Value, b.Value)
	})
	return dedupeModels(choices), nil
}

func isUsefulTranslationModel(model openRouterModel) bool {
	if strings.Contains(model.ID, ":free") || strings.HasPrefix(model.ID, "~") {
		return false
	}
	if !containsString(model.Architecture.InputModalities, "text") || !containsString(model.Architecture.OutputModalities, "text") {
		return false
	}
	if !containsString(model.SupportedParameters, "response_format") && !containsString(model.SupportedParameters, "structured_outputs") {
		return false
	}
	prompt := pricePerMillion(model.Pricing.Prompt)
	completion := pricePerMillion(model.Pricing.Completion)
	if prompt < 0 || completion < 0 || prompt > 30 || completion > 150 {
		return false
	}
	text := strings.ToLower(model.ID + " " + model.Name + " " + model.Description)
	if strings.Contains(text, "embedding") || strings.Contains(text, "image generation") || strings.Contains(text, "ocr") {
		return false
	}
	return true
}

func scoreOpenRouterModel(model openRouterModel) float64 {
	text := strings.ToLower(model.ID + " " + model.Name + " " + model.Description)
	score := float64(model.Created) / 86400
	score += minFloat(float64(model.ContextLength)/200000, 6)
	prompt := pricePerMillion(model.Pricing.Prompt)
	completion := pricePerMillion(model.Pricing.Completion)
	total := prompt + completion
	switch {
	case total <= 1:
		score += 9
	case total <= 4:
		score += 7
	case total <= 12:
		score += 5
	default:
		score += 2
	}
	if strings.Contains(text, "translate") || strings.Contains(text, "multilingual") {
		score += 8
	}
	if strings.Contains(text, "reasoning") || strings.Contains(text, "agent") || strings.Contains(text, "instruction") {
		score += 5
	}
	for provider, bonus := range map[string]float64{
		"anthropic/claude": 7,
		"google/gemini":    7,
		"openai/gpt":       6,
		"qwen/":            5,
		"mistralai/":       4,
		"moonshotai/":      4,
		"x-ai/":            3,
	} {
		if strings.Contains(model.ID, provider) {
			score += bonus
		}
	}
	if strings.Contains(text, "flash") || strings.Contains(text, "fast") || strings.Contains(text, "mini") || strings.Contains(text, "lite") {
		score += 3
	}
	return score
}

func modelLabel(model openRouterModel, score float64) string {
	prompt := pricePerMillion(model.Pricing.Prompt)
	completion := pricePerMillion(model.Pricing.Completion)
	tier := "balanced"
	if prompt+completion <= 2 {
		tier = "cheap"
	} else if strings.Contains(strings.ToLower(model.Name), "flash") || strings.Contains(strings.ToLower(model.Name), "fast") {
		tier = "fast"
	} else if strings.Contains(strings.ToLower(model.Name), "opus") || strings.Contains(strings.ToLower(model.Name), "pro") {
		tier = "smart"
	}
	return fmt.Sprintf("%s · %s · $%.2f/$%.2f per 1M", model.Name, tier, prompt, completion)
}

func fallbackModelOptions() []modelChoice {
	return []modelChoice{
		{Label: "OpenRouter Auto · latest routed model", Value: "openrouter/auto"},
		{Label: "Claude Opus Latest · smart", Value: "~anthropic/claude-opus-latest"},
		{Label: "GPT Chat Latest · latest OpenAI chat", Value: "openai/gpt-chat-latest"},
	}
}

func dedupeModels(choices []modelChoice) []modelChoice {
	seen := map[string]bool{}
	result := []modelChoice{}
	for _, choice := range choices {
		if seen[choice.Value] {
			continue
		}
		seen[choice.Value] = true
		result = append(result, choice)
	}
	return result
}

func containsModel(choices []modelChoice, value string) bool {
	for _, choice := range choices {
		if choice.Value == value {
			return true
		}
	}
	return false
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func pricePerMillion(value string) float64 {
	price, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 999
	}
	return price * 1_000_000
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func chooseInitLanguages(config provider.ProjectConfig) ([]string, error) {
	selected := []string{}
	catalogs, err := loadInitCatalogs(config)
	if err == nil {
		selected = catalog.ConfiguredLanguages(catalogs, nil)
	}

	options := []huh.Option[string]{}
	seen := map[string]bool{}
	addOption := func(locale popularLocale, selected bool) {
		if seen[locale.Code] {
			return
		}
		seen[locale.Code] = true
		label := fmt.Sprintf("%s %s  %s", flagForLocale(locale), locale.Code, locale.Name)
		options = append(options, huh.NewOption(label, locale.Code).Selected(selected))
	}

	for _, code := range selected {
		addOption(popularLocale{Code: code, Name: languageName(code, config.LanguageNames)}, true)
	}
	for _, locale := range popularLocales {
		addOption(locale, false)
	}
	if len(options) == 0 {
		return selected, nil
	}
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Languages").
				Description("Space toggles a locale. Return accepts the preselected catalog languages. Type / to filter.").
				Options(options...).
				Height(14).
				Filterable(true).
				Value(&selected),
		),
	).WithTheme(initTheme()).WithWidth(88).Run(); err != nil {
		return nil, err
	}
	return selected, nil
}

func loadInitCatalogs(config provider.ProjectConfig) ([]*catalog.Catalog, error) {
	paths := append([]string{}, config.Catalogs...)
	for _, root := range config.Discover {
		discovered, err := catalog.DiscoverCatalogs(root)
		if err != nil {
			continue
		}
		paths = append(paths, discovered...)
	}
	paths = uniqueStrings(paths)
	if len(paths) == 0 {
		return nil, errors.New("no catalogs found")
	}
	return catalog.LoadMany(paths)
}

type popularLocale struct {
	Code string
	Name string
	Flag string
}

var popularLocales = []popularLocale{
	{Code: "en-GB", Name: "English (United Kingdom)", Flag: "🇬🇧"},
	{Code: "ar", Name: "Arabic", Flag: "🇸🇦"},
	{Code: "ru", Name: "Russian", Flag: "🇷🇺"},
	{Code: "it", Name: "Italian", Flag: "🇮🇹"},
	{Code: "tr", Name: "Turkish", Flag: "🇹🇷"},
	{Code: "en-AU", Name: "English (Australia)", Flag: "🇦🇺"},
	{Code: "en-CA", Name: "English (Canada)", Flag: "🇨🇦"},
	{Code: "hi", Name: "Hindi", Flag: "🇮🇳"},
	{Code: "nl", Name: "Dutch (Netherlands)", Flag: "🇳🇱"},
	{Code: "fa", Name: "Persian (Iran)", Flag: "🇮🇷"},
	{Code: "id", Name: "Indonesian", Flag: "🇮🇩"},
	{Code: "vi", Name: "Vietnamese", Flag: "🇻🇳"},
	{Code: "ms", Name: "Malay", Flag: "🇲🇾"},
	{Code: "pl", Name: "Polish", Flag: "🇵🇱"},
	{Code: "bn", Name: "Bangla", Flag: "🇧🇩"},
	{Code: "sv", Name: "Swedish", Flag: "🇸🇪"},
	{Code: "zh-Hant", Name: "Chinese, Traditional", Flag: "🇹🇼"},
}

func flagForLocale(locale popularLocale) string {
	if locale.Flag != "" {
		return locale.Flag
	}
	base := locale.Code
	if index := strings.Index(base, "-"); index >= 0 {
		base = base[:index]
	}
	flags := map[string]string{
		"ar":      "🇸🇦",
		"bn":      "🇧🇩",
		"de":      "🇩🇪",
		"en":      "🇺🇸",
		"en-AU":   "🇦🇺",
		"en-CA":   "🇨🇦",
		"en-GB":   "🇬🇧",
		"es":      "🇪🇸",
		"es-419":  "🌎",
		"es-MX":   "🇲🇽",
		"es-US":   "🇺🇸",
		"fa":      "🇮🇷",
		"fr":      "🇫🇷",
		"hi":      "🇮🇳",
		"id":      "🇮🇩",
		"it":      "🇮🇹",
		"ja":      "🇯🇵",
		"ko":      "🇰🇷",
		"ms":      "🇲🇾",
		"nl":      "🇳🇱",
		"pl":      "🇵🇱",
		"pt-BR":   "🇧🇷",
		"pt-PT":   "🇵🇹",
		"ru":      "🇷🇺",
		"sv":      "🇸🇪",
		"tr":      "🇹🇷",
		"vi":      "🇻🇳",
		"zh-Hans": "🇨🇳",
		"zh-Hant": "🇹🇼",
	}
	if flag := flags[locale.Code]; flag != "" {
		return flag
	}
	if flag := flags[base]; flag != "" {
		return flag
	}
	return "🌐"
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func uniqueInOrder(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiFlag) Set(value string) error {
	if value == "" {
		return nil
	}
	*m = append(*m, value)
	return nil
}

func languageName(language string, overrides map[string]string) string {
	if overrides[language] != "" {
		return overrides[language]
	}
	if name := LanguageNames[language]; name != "" {
		return name
	}
	return language
}

func envDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func printHelp(out io.Writer) {
	fmt.Fprint(out, strings.TrimSpace(`
strata translates missing Xcode string catalog entries.

Usage:
  strata init
  strata run
  strata translate [options] <Localizable.xcstrings...>

Options:
  --language <code>     Target BCP-47 language. Defaults to languages already in the catalog.
  --name <code=name>    Override language display name.
  --model <name>        OpenRouter model picker. Defaults to OPENROUTER_MODEL or google/gemini-2.5-flash.
  --config <path>       JSON project config. Defaults to strata.json when present.
  --batch-size <n>      Strings per provider request. Default: 40.
  --concurrency <n>     Provider requests to run in parallel. Default: 4.
  --force               Re-translate existing target values.
  --dry-run             Count missing strings without writing or calling the provider.
  --provider <name>     openrouter or mock. Default: openrouter.
  --discover <path>     Recursively find .xcstrings files. Repeat for multiple roots.
  --timeout <seconds>   Provider timeout. Default: 120.
  --version             Print version.
`)+"\n")
}
