package catalog

import (
	"slices"
	"strings"
)

// CLDR cardinal plural categories that Xcode expects per language.
var pluralCategoriesByLanguage = map[string][]string{
	"ar":      {"zero", "one", "two", "few", "many", "other"},
	"ca":      {"one", "many", "other"},
	"cs":      {"one", "few", "many", "other"},
	"da":      {"one", "other"},
	"de":      {"one", "other"},
	"el":      {"one", "other"},
	"en":      {"one", "other"},
	"es":      {"one", "many", "other"},
	"fi":      {"one", "other"},
	"fr":      {"one", "many", "other"},
	"he":      {"one", "two", "other"},
	"hi":      {"one", "other"},
	"hr":      {"one", "few", "other"},
	"hu":      {"one", "other"},
	"id":      {"other"},
	"it":      {"one", "many", "other"},
	"ja":      {"other"},
	"ko":      {"other"},
	"ms":      {"other"},
	"nb":      {"one", "other"},
	"nl":      {"one", "other"},
	"pl":      {"one", "few", "many", "other"},
	"pt":      {"one", "many", "other"},
	"ro":      {"one", "few", "other"},
	"ru":      {"one", "few", "many", "other"},
	"sk":      {"one", "few", "many", "other"},
	"sv":      {"one", "other"},
	"th":      {"other"},
	"tr":      {"one", "other"},
	"uk":      {"one", "few", "many", "other"},
	"vi":      {"other"},
	"zh":      {"other"},
	"zh-Hans": {"other"},
	"zh-Hant": {"other"},
}

// PluralCategories returns the plural categories to write for language. It
// falls back to the source categories for an unknown language.
func PluralCategories(language string, sourceCategories []string) []string {
	if categories, ok := pluralCategoriesByLanguage[language]; ok {
		return categories
	}
	if base, _, ok := strings.Cut(language, "-"); ok {
		if categories, ok := pluralCategoriesByLanguage[base]; ok {
			return categories
		}
	}
	categories := slices.Clone(sourceCategories)
	slices.Sort(categories)
	return categories
}

func isPluralPath(path []string) bool {
	count := len(path)
	return count >= 2 && path[count-1] == "plural" && path[count-2] == "variations"
}

// pluralSourceChild returns the source form for a target category. A missing
// category uses the source "other" form.
func pluralSourceChild(plural map[string]any, category string) map[string]any {
	if child, ok := plural[category].(map[string]any); ok {
		return child
	}
	if child, ok := plural["other"].(map[string]any); ok {
		return child
	}
	keys := sortedKeys(plural)
	for index := len(keys) - 1; index >= 0; index-- {
		if child, ok := plural[keys[index]].(map[string]any); ok {
			return child
		}
	}
	return nil
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
