package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type Catalog struct {
	Path           string
	Source         string
	Strings        map[string]*Entry
	OrderedKeys    []string
	originalObject map[string]any
}

type Entry struct {
	Comment         string
	ShouldTranslate bool
	Localizations   map[string]*Localization
	raw             map[string]any
}

type Localization struct {
	StringUnit *StringUnit `json:"stringUnit,omitempty"`
	raw        map[string]any
}

type StringUnit struct {
	State string `json:"state,omitempty"`
	Value string `json:"value,omitempty"`
}

type Item struct {
	ID           string         `json:"id"`
	Key          string         `json:"key"`
	Source       string         `json:"source"`
	Comment      string         `json:"comment,omitempty"`
	Placeholders []string       `json:"placeholders"`
	VariantPath  []string       `json:"variant_path,omitempty"`
	UsageContext []UsageContext `json:"usage_context,omitempty"`
}

type Coverage struct {
	Total      int
	Translated int
	Missing    int
}

type UsageContext struct {
	File       string   `json:"file,omitempty"`
	Type       string   `json:"type,omitempty"`
	Function   string   `json:"function,omitempty"`
	View       string   `json:"view,omitempty"`
	Line       int      `json:"line,omitempty"`
	References []string `json:"references,omitempty"`
	Snippet    string   `json:"snippet,omitempty"`
	Role       string   `json:"role,omitempty"`
	// Interpolations lists the Swift expressions that fill the format
	// specifiers, in order.
	Interpolations []string `json:"interpolations,omitempty"`
}

type stringLeaf struct {
	Path   []string
	Source string
}

func Load(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("%s: parse xcstrings: %w", path, err)
	}

	source, _ := root["sourceLanguage"].(string)
	if source == "" {
		source = "en"
	}

	rawStrings, ok := root["strings"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: missing strings object", path)
	}

	keys := make([]string, 0, len(rawStrings))
	stringsByKey := make(map[string]*Entry, len(rawStrings))
	for key, value := range rawStrings {
		entryMap, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: string %q is not an object", path, key)
		}
		keys = append(keys, key)
		entry := &Entry{raw: entryMap, Localizations: map[string]*Localization{}, ShouldTranslate: true}
		if comment, ok := entryMap["comment"].(string); ok {
			entry.Comment = comment
		}
		if shouldTranslate, ok := entryMap["shouldTranslate"].(bool); ok {
			entry.ShouldTranslate = shouldTranslate
		}
		if rawLocalizations, ok := entryMap["localizations"].(map[string]any); ok {
			for language, rawLocalization := range rawLocalizations {
				locMap, ok := rawLocalization.(map[string]any)
				if !ok {
					continue
				}
				loc := &Localization{raw: locMap, StringUnit: parseStringUnit(locMap["stringUnit"])}
				entry.Localizations[language] = loc
			}
		}
		stringsByKey[key] = entry
	}
	slices.Sort(keys)

	return &Catalog{
		Path:           path,
		Source:         source,
		Strings:        stringsByKey,
		OrderedKeys:    keys,
		originalObject: root,
	}, nil
}

func LoadMany(paths []string) ([]*Catalog, error) {
	catalogs := make([]*Catalog, 0, len(paths))
	for _, path := range paths {
		catalog, err := Load(path)
		if err != nil {
			return nil, err
		}
		catalogs = append(catalogs, catalog)
	}
	return catalogs, nil
}

func ConfiguredLanguages(catalogs []*Catalog, explicit []string) []string {
	if len(explicit) > 0 {
		return uniqueInOrder(explicit)
	}
	seen := map[string]bool{}
	for _, catalog := range catalogs {
		for _, entry := range catalog.Strings {
			for language := range entry.Localizations {
				if language != catalog.Source && language != "en" {
					seen[language] = true
				}
			}
		}
	}
	languages := make([]string, 0, len(seen))
	for language := range seen {
		languages = append(languages, language)
	}
	slices.Sort(languages)
	return languages
}

func (c *Catalog) SourceText(key string, entry *Entry) string {
	if loc := c.sourceLocalization(entry); loc != nil && loc.Value() != "" {
		return loc.Value()
	}
	return key
}

func (c *Catalog) MissingItems(language string, force bool) []Item {
	items := []Item{}
	for _, key := range c.OrderedKeys {
		entry := c.Strings[key]
		if !entry.ShouldTranslate {
			continue
		}
		targetLoc := entry.Localizations[language]
		for _, leaf := range c.targetLeaves(key, entry, language) {
			if strings.TrimSpace(leaf.Source) == "" {
				continue
			}
			if !force && hasTranslation(targetLoc, leaf.Path) {
				continue
			}
			var variantPath []string
			if !slices.Equal(leaf.Path, []string{"stringUnit"}) {
				variantPath = leaf.Path
			}
			items = append(items, Item{
				ID:           fmt.Sprintf("%d", len(items)),
				Key:          key,
				Source:       leaf.Source,
				Comment:      entry.Comment,
				Placeholders: Placeholders(leaf.Source),
				VariantPath:  variantPath,
			})
		}
	}
	return items
}

func (c *Catalog) Coverage(language string) Coverage {
	coverage := Coverage{}
	for _, key := range c.OrderedKeys {
		entry := c.Strings[key]
		if !entry.ShouldTranslate {
			continue
		}
		targetLoc := entry.Localizations[language]
		for _, leaf := range c.targetLeaves(key, entry, language) {
			if strings.TrimSpace(leaf.Source) == "" {
				continue
			}
			coverage.Total++
			if hasTranslation(targetLoc, leaf.Path) {
				coverage.Translated++
			}
		}
	}
	coverage.Missing = coverage.Total - coverage.Translated
	return coverage
}

func (c *Catalog) ApplyTranslation(language, key string, variantPath []string, value string) {
	entry := c.Strings[key]
	if entry.Localizations == nil {
		entry.Localizations = map[string]*Localization{}
	}
	if len(variantPath) == 0 {
		variantPath = []string{"stringUnit"}
	}

	loc := entry.Localizations[language]
	if loc == nil {
		loc = &Localization{}
		entry.Localizations[language] = loc
	}
	if loc.raw == nil {
		loc.raw = map[string]any{}
	}
	var sourceRaw map[string]any
	if sourceLoc := c.sourceLocalization(entry); sourceLoc != nil {
		sourceRaw = sourceLoc.raw
	}
	if variantPath[0] == "variations" {
		delete(loc.raw, "stringUnit")
	}
	setStringUnitLeaf(loc.raw, sourceRaw, variantPath, value)
	loc.StringUnit = parseStringUnit(loc.raw["stringUnit"])
}

func (c *Catalog) Write() error {
	root := cloneMap(c.originalObject)
	stringsObject := map[string]any{}
	for _, key := range c.OrderedKeys {
		entry := c.Strings[key]
		entryObject := cloneMap(entry.raw)
		localizationsObject := map[string]any{}
		for language, loc := range entry.Localizations {
			if loc.raw != nil {
				localizationsObject[language] = cloneMap(loc.raw)
			}
		}
		if len(localizationsObject) > 0 {
			entryObject["localizations"] = localizationsObject
		}
		stringsObject[key] = entryObject
	}
	root["strings"] = stringsObject

	data, err := MarshalXcode(root)
	if err != nil {
		return err
	}
	return os.WriteFile(c.Path, data, 0644)
}

func (l *Localization) Value() string {
	if l == nil || l.StringUnit == nil {
		return ""
	}
	return l.StringUnit.Value
}

func (c *Catalog) sourceLocalization(entry *Entry) *Localization {
	if loc := entry.Localizations[c.Source]; loc != nil {
		return loc
	}
	return entry.Localizations["en"]
}

// targetLeaves lists the string units that language needs for key. Plural
// variations use the target language's categories.
func (c *Catalog) targetLeaves(key string, entry *Entry, language string) []stringLeaf {
	if sourceLoc := c.sourceLocalization(entry); sourceLoc != nil && sourceLoc.raw != nil {
		if leaves := collectTargetLeaves(sourceLoc.raw, nil, language); len(leaves) > 0 {
			return leaves
		}
	}
	return []stringLeaf{{Path: []string{"stringUnit"}, Source: c.SourceText(key, entry)}}
}

func collectTargetLeaves(node map[string]any, path []string, language string) []stringLeaf {
	var leaves []stringLeaf
	if isPluralPath(path) {
		for _, category := range PluralCategories(language, sortedKeys(node)) {
			if child := pluralSourceChild(node, category); child != nil {
				leaves = append(leaves, collectTargetLeaves(child, appendPath(path, category), language)...)
			}
		}
		return leaves
	}
	if unit := parseStringUnit(node["stringUnit"]); unit != nil {
		leaves = append(leaves, stringLeaf{Path: appendPath(path, "stringUnit"), Source: unit.Value})
	}
	for _, key := range sortedKeys(node) {
		if key == "stringUnit" {
			continue
		}
		if child, ok := node[key].(map[string]any); ok {
			leaves = append(leaves, collectTargetLeaves(child, appendPath(path, key), language)...)
		}
	}
	return leaves
}

func hasTranslation(localization *Localization, path []string) bool {
	unit, ok := findStringUnitLeaf(localization, path)
	return ok && unit.Value != ""
}

func findStringUnitLeaf(localization *Localization, path []string) (StringUnit, bool) {
	if localization == nil || localization.raw == nil {
		return StringUnit{}, false
	}
	current := localization.raw
	for index, part := range path {
		child, ok := current[part].(map[string]any)
		if !ok {
			return StringUnit{}, false
		}
		if index == len(path)-1 {
			return *parseStringUnit(child), true
		}
		current = child
	}
	return StringUnit{}, false
}

// setStringUnitLeaf writes a translated string unit at path. It creates missing
// parent objects and copies their scalar fields, such as argNum and
// formatSpecifier, from the matching source node.
func setStringUnitLeaf(root, source map[string]any, path []string, value string) {
	current := root
	for index, part := range path {
		if index == len(path)-1 {
			current[part] = map[string]any{
				"state": "translated",
				"value": value,
			}
			return
		}
		var sourceChild map[string]any
		if source != nil {
			if isPluralPath(path[:index]) {
				sourceChild = pluralSourceChild(source, part)
			} else {
				sourceChild, _ = source[part].(map[string]any)
			}
		}
		child, ok := current[part].(map[string]any)
		if !ok {
			child = scalarFields(sourceChild)
			current[part] = child
		}
		current = child
		source = sourceChild
	}
}

func scalarFields(node map[string]any) map[string]any {
	fields := map[string]any{}
	for key, value := range node {
		if _, isMap := value.(map[string]any); !isMap {
			fields[key] = cloneValue(value)
		}
	}
	return fields
}

func parseStringUnit(value any) *StringUnit {
	rawUnit, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	unit := &StringUnit{}
	unit.State, _ = rawUnit["state"].(string)
	unit.Value, _ = rawUnit["value"].(string)
	return unit
}

func appendPath(path []string, part string) []string {
	return append(slices.Clone(path), part)
}

func HasLinguisticText(text string) bool {
	for _, char := range text {
		if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || char > 127 {
			return true
		}
	}
	return false
}

func DiscoverCatalogs(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".build" || name == ".derived" || name == ".macro-build" || name == "build" || name == "DerivedData" || name == "node_modules" || name == "Pods" || name == "Carthage" {
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".xcstrings") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(paths)
	if len(paths) == 0 {
		return nil, errors.New("no .xcstrings files found")
	}
	return paths, nil
}

func uniqueInOrder(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func cloneMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = cloneValue(value)
	}
	return output
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		output := make([]any, len(typed))
		for index, element := range typed {
			output[index] = cloneValue(element)
		}
		return output
	default:
		return typed
	}
}
