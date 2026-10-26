package catalog

import (
	"slices"
	"strconv"
)

// App Shortcuts catalogs store phrases as a stringSet: one state and an
// ordered list of values. Each value is a separate item with the path
// [..., "stringSet", "values", "<index>"].

func stringSetValues(node map[string]any) ([]string, bool) {
	set, ok := node["stringSet"].(map[string]any)
	if !ok {
		return nil, false
	}
	rawValues, _ := set["values"].([]any)
	values := make([]string, len(rawValues))
	for index, rawValue := range rawValues {
		values[index], _ = rawValue.(string)
	}
	return values, true
}

func stringSetPath(path []string, index int) []string {
	return append(slices.Clone(path), "stringSet", "values", strconv.Itoa(index))
}

// splitStringSetPath returns the parent path and value index when path points
// into a stringSet.
func splitStringSetPath(path []string) ([]string, int, bool) {
	count := len(path)
	if count < 3 || path[count-3] != "stringSet" || path[count-2] != "values" {
		return nil, 0, false
	}
	index, err := strconv.Atoi(path[count-1])
	if err != nil || index < 0 {
		return nil, 0, false
	}
	return path[:count-3], index, true
}

func hasStringSet(localizations map[string]*Localization) bool {
	for _, loc := range localizations {
		if loc != nil && loc.raw != nil && loc.raw["stringSet"] != nil {
			return true
		}
	}
	return false
}

// setStringSetValue writes value at index in the stringSet under parent. The
// set has size values; the state stays "new" until every value is filled.
func setStringSetValue(parent map[string]any, index, size int, value string) {
	set, ok := parent["stringSet"].(map[string]any)
	if !ok {
		set = map[string]any{}
		parent["stringSet"] = set
	}
	rawValues, _ := set["values"].([]any)
	size = max(size, index+1, len(rawValues))
	values := make([]any, size)
	for position := range values {
		values[position] = ""
		if position < len(rawValues) {
			values[position] = rawValues[position]
		}
	}
	values[index] = value
	set["values"] = values
	set["state"] = "translated"
	for _, element := range values {
		if text, _ := element.(string); text == "" {
			set["state"] = "new"
			break
		}
	}
}
