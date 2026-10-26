package catalog

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// EntryStructure shows the model how an entry with %#@name@ substitutions is
// built: the root text, each substitution's English forms, and the rendered
// English sentences.
type EntryStructure struct {
	Root             string                       `json:"root"`
	Substitutions    map[string]map[string]string `json:"substitutions"`
	RenderedExamples []string                     `json:"rendered_examples"`
}

var substitutionTokenPattern = regexp.MustCompile(`%#@([A-Za-z_][A-Za-z0-9_]*)@`)

// IsSubstitutionPath reports whether path points into a substitution and
// returns the substitution name.
func IsSubstitutionPath(path []string) (string, bool) {
	if len(path) >= 2 && path[0] == "substitutions" {
		return path[1], true
	}
	return "", false
}

func (c *Catalog) entryStructure(entry *Entry) *EntryStructure {
	sourceLoc := c.sourceLocalization(entry)
	if sourceLoc == nil || sourceLoc.raw == nil {
		return nil
	}
	substitutions, ok := sourceLoc.raw["substitutions"].(map[string]any)
	if !ok || len(substitutions) == 0 {
		return nil
	}
	root := sourceLoc.Value()
	structure := &EntryStructure{Root: root, Substitutions: map[string]map[string]string{}}
	for name, rawSubstitution := range substitutions {
		forms := map[string]string{}
		substitution, _ := rawSubstitution.(map[string]any)
		variations, _ := substitution["variations"].(map[string]any)
		plural, _ := variations["plural"].(map[string]any)
		for category, rawForm := range plural {
			form, _ := rawForm.(map[string]any)
			if unit := parseStringUnit(form["stringUnit"]); unit != nil {
				forms[category] = unit.Value
			}
		}
		structure.Substitutions[name] = forms
	}
	for _, example := range []struct{ category, number string }{{"one", "1"}, {"other", "3"}} {
		rendered := substitutionTokenPattern.ReplaceAllStringFunc(root, func(token string) string {
			forms := structure.Substitutions[substitutionTokenPattern.FindStringSubmatch(token)[1]]
			form, ok := forms[example.category]
			if !ok {
				form, ok = forms["other"]
			}
			if !ok {
				return token
			}
			return strings.ReplaceAll(form, "%arg", example.number)
		})
		if !slices.Contains(structure.RenderedExamples, rendered) {
			structure.RenderedExamples = append(structure.RenderedExamples, rendered)
		}
	}
	return structure
}

// CheckSubstitutionRepetition rejects a root translation that repeats words
// from a substitution form outside the %#@name@ tokens. forms maps each
// substitution name to its translated forms.
func CheckSubstitutionRepetition(root string, forms map[string][]string) error {
	outside := stripPlaceholders(substitutionTokenPattern.ReplaceAllString(root, " "))
	outsideWords := words(outside)
	outsideCompact := compactCJK(outside)
	for _, name := range sortedFormNames(forms) {
		for _, form := range forms[name] {
			text := stripPlaceholders(form)
			if repeated := repeatedText(text, outside, outsideWords, outsideCompact); repeated != "" {
				return fmt.Errorf("the root repeats %q from substitution %q; keep only the text outside %%#@%s@ in the root and put the full phrase in each substitution form", repeated, name, name)
			}
		}
	}
	return nil
}

func repeatedText(form, outside string, outsideWords []string, outsideCompact string) string {
	trimmed := strings.TrimSpace(form)
	if len([]rune(trimmed)) >= 2 && HasLinguisticText(trimmed) && strings.Contains(outside, trimmed) {
		return trimmed
	}
	formWords := words(form)
	for start := 0; start+3 <= len(formWords); start++ {
		run := formWords[start : start+3]
		for index := 0; index+3 <= len(outsideWords); index++ {
			if slices.Equal(run, outsideWords[index:index+3]) {
				return strings.Join(run, " ")
			}
		}
	}
	compact := []rune(compactCJK(form))
	for start := 0; start+4 <= len(compact); start++ {
		window := compact[start : start+4]
		if allCJK(window) && strings.Contains(outsideCompact, string(window)) {
			return string(window)
		}
	}
	if len([]rune(trimmed)) >= 2 && strings.Contains(outsideCompact, compactCJK(trimmed)) && allCJK([]rune(compactCJK(trimmed))) {
		return trimmed
	}
	return ""
}

func stripPlaceholders(text string) string {
	return PlaceholderPattern.ReplaceAllString(strings.ReplaceAll(text, "%arg", " "), " ")
}

func words(text string) []string {
	return strings.FieldsFunc(text, func(char rune) bool {
		return !(unicode.IsLetter(char) || unicode.IsDigit(char)) || isCJK(char)
	})
}

func compactCJK(text string) string {
	return strings.Map(func(char rune) rune {
		if unicode.IsSpace(char) {
			return -1
		}
		return char
	}, text)
}

func isCJK(char rune) bool {
	return unicode.In(char, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

func allCJK(runes []rune) bool {
	if len(runes) == 0 {
		return false
	}
	for _, char := range runes {
		if !isCJK(char) {
			return false
		}
	}
	return true
}

func sortedFormNames(forms map[string][]string) []string {
	names := make([]string, 0, len(forms))
	for name := range forms {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
