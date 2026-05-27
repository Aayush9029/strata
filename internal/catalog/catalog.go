package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var PlaceholderPattern = regexp.MustCompile(`%#@[A-Za-z_][A-Za-z0-9_]*@|%(?:(?:\d+\$)?(?:[-+#0]*)(?:\d+|\*)?(?:\.(?:\d+|\*))?(?:hh|ll|[hlLzjtq])?[diouxXfFeEgGaAcCsSp@])|\\[nrt"]`)

type Catalog struct {
	Path           string
	Source         string
	Strings        map[string]*Entry
	OrderedKeys    []string
	originalObject map[string]any
}

type Entry struct {
	Comment       string
	Localizations map[string]*Localization
	raw           map[string]any
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

type UsageContext struct {
	File       string   `json:"file,omitempty"`
	Type       string   `json:"type,omitempty"`
	Function   string   `json:"function,omitempty"`
	View       string   `json:"view,omitempty"`
	Line       int      `json:"line,omitempty"`
	References []string `json:"references,omitempty"`
	Snippet    string   `json:"snippet,omitempty"`
}

type stringLeaf struct {
	Path []string
	Unit StringUnit
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
		entry := &Entry{raw: entryMap, Localizations: map[string]*Localization{}}
		if comment, ok := entryMap["comment"].(string); ok {
			entry.Comment = comment
		}
		if rawLocalizations, ok := entryMap["localizations"].(map[string]any); ok {
			for language, rawLocalization := range rawLocalizations {
				locMap, ok := rawLocalization.(map[string]any)
				if !ok {
					continue
				}
				loc := &Localization{raw: locMap}
				if rawUnit, ok := locMap["stringUnit"].(map[string]any); ok {
					unit := &StringUnit{}
					if state, ok := rawUnit["state"].(string); ok {
						unit.State = state
					}
					if text, ok := rawUnit["value"].(string); ok {
						unit.Value = text
					}
					loc.StringUnit = unit
				}
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
		sourceLoc := c.sourceLocalization(entry)
		targetLoc := entry.Localizations[language]
		sourceLeaves := collectStringUnitLeaves(sourceLoc)

		if len(sourceLeaves) == 0 {
			source := c.SourceText(key, entry)
			if strings.TrimSpace(source) == "" {
				continue
			}
			if targetLoc != nil && targetLoc.Value() != "" && !force {
				continue
			}
			items = append(items, Item{
				ID:           fmt.Sprintf("%d", len(items)),
				Key:          key,
				Source:       source,
				Comment:      entry.Comment,
				Placeholders: Placeholders(source),
			})
			continue
		}

		for _, sourceLeaf := range sourceLeaves {
			source := sourceLeaf.Unit.Value
			if strings.TrimSpace(source) == "" {
				continue
			}
			if !force {
				if targetLeaf, ok := findStringUnitLeaf(targetLoc, sourceLeaf.Path); ok && targetLeaf.Value != "" {
					continue
				}
			}
			items = append(items, Item{
				ID:           fmt.Sprintf("%d", len(items)),
				Key:          key,
				Source:       source,
				Comment:      entry.Comment,
				Placeholders: Placeholders(source),
				VariantPath:  sourceLeaf.Path,
			})
		}
	}
	return items
}

func (c *Catalog) ApplyTranslation(language, key string, variantPath []string, value string) {
	entry := c.Strings[key]
	if entry.Localizations == nil {
		entry.Localizations = map[string]*Localization{}
	}

	if len(variantPath) == 0 || (len(variantPath) == 1 && variantPath[0] == "stringUnit") {
		entry.Localizations[language] = &Localization{
			StringUnit: &StringUnit{State: "translated", Value: value},
			raw: map[string]any{
				"stringUnit": map[string]any{
					"state": "translated",
					"value": value,
				},
			},
		}
		return
	}

	loc := entry.Localizations[language]
	if loc == nil || loc.raw == nil {
		sourceLoc := c.sourceLocalization(entry)
		loc = &Localization{raw: cloneMap(sourceLoc.raw)}
		entry.Localizations[language] = loc
	}
	setStringUnitLeaf(loc.raw, variantPath, value)
	loc.StringUnit = nil
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
			if loc.StringUnit != nil {
				localizationsObject[language] = map[string]any{
					"stringUnit": map[string]any{
						"state": loc.StringUnit.State,
						"value": loc.StringUnit.Value,
					},
				}
			}
		}
		if len(localizationsObject) > 0 {
			entryObject["localizations"] = localizationsObject
		}
		stringsObject[key] = entryObject
	}
	root["strings"] = stringsObject

	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
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

func collectStringUnitLeaves(localization *Localization) []stringLeaf {
	if localization == nil {
		return nil
	}
	if localization.StringUnit != nil {
		return []stringLeaf{{Path: []string{"stringUnit"}, Unit: *localization.StringUnit}}
	}
	return collectStringUnitLeavesInMap(localization.raw, nil)
}

func collectStringUnitLeavesInMap(current map[string]any, path []string) []stringLeaf {
	if current == nil {
		return nil
	}
	if rawUnit, ok := current["stringUnit"].(map[string]any); ok {
		unit := StringUnit{}
		if state, ok := rawUnit["state"].(string); ok {
			unit.State = state
		}
		if value, ok := rawUnit["value"].(string); ok {
			unit.Value = value
		}
		return []stringLeaf{{Path: append(append([]string{}, path...), "stringUnit"), Unit: unit}}
	}

	keys := make([]string, 0, len(current))
	for key := range current {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var leaves []stringLeaf
	for _, key := range keys {
		child, ok := current[key].(map[string]any)
		if !ok {
			continue
		}
		leaves = append(leaves, collectStringUnitLeavesInMap(child, append(path, key))...)
	}
	return leaves
}

func findStringUnitLeaf(localization *Localization, path []string) (StringUnit, bool) {
	if localization == nil || localization.raw == nil {
		return StringUnit{}, false
	}
	current := localization.raw
	for index, part := range path {
		value, ok := current[part]
		if !ok {
			return StringUnit{}, false
		}
		child, ok := value.(map[string]any)
		if !ok {
			return StringUnit{}, false
		}
		if index == len(path)-1 {
			unit := StringUnit{}
			if state, ok := child["state"].(string); ok {
				unit.State = state
			}
			if text, ok := child["value"].(string); ok {
				unit.Value = text
			}
			return unit, true
		}
		current = child
	}
	return StringUnit{}, false
}

func setStringUnitLeaf(root map[string]any, path []string, value string) {
	current := root
	for index, part := range path {
		if index == len(path)-1 {
			current[part] = map[string]any{
				"state": "translated",
				"value": value,
			}
			return
		}
		child, ok := current[part].(map[string]any)
		if !ok {
			child = map[string]any{}
			current[part] = child
		}
		current = child
	}
}

func HasLinguisticText(text string) bool {
	for _, char := range text {
		if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || char > 127 {
			return true
		}
	}
	return false
}

func Placeholders(text string) []string {
	return PlaceholderPattern.FindAllString(text, -1)
}

func ValidateTranslation(source, translation string) error {
	expected := Placeholders(source)
	actual := Placeholders(translation)
	if !slices.Equal(expected, actual) {
		return fmt.Errorf("placeholder mismatch: expected %v, got %v", expected, actual)
	}
	return nil
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
