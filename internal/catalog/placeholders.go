package catalog

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var PlaceholderPattern = regexp.MustCompile(`%%|%#@[A-Za-z_][A-Za-z0-9_]*@|%arg\b|\$\{[A-Za-z_][A-Za-z0-9_.]*\}|%(?:(?:\d+\$)?(?:[-+#0]*)(?:\d+|\*)?(?:\.(?:\d+|\*))?(?:hh|ll|[hlLzjtq])?[diouxXfFeEgGaAcCsSp@])|\\[nrt"]`)

var specifierPattern = regexp.MustCompile(`^%(?:(\d+)\$)?[-+#0]*(?:\d+|\*)?(?:\.(?:\d+|\*))?((?:hh|ll|[hlLzjtq])?[diouxXfFeEgGaAcCsSp@])$`)

type formatArgument struct {
	Index      int
	Type       string
	Positional bool
}

func (a formatArgument) String() string {
	return fmt.Sprintf("%%%d$%s", a.Index, a.Type)
}

type placeholderSummary struct {
	Arguments []formatArgument
	Tokens    []string
}

// Placeholders returns the format specifiers, substitution tokens, and escape
// sequences in text. A literal %% is not a placeholder.
func Placeholders(text string) []string {
	var placeholders []string
	for _, match := range PlaceholderPattern.FindAllString(text, -1) {
		if match != "%%" {
			placeholders = append(placeholders, match)
		}
	}
	return placeholders
}

// IsFormatSpecifier reports whether a placeholder consumes a format argument.
func IsFormatSpecifier(placeholder string) bool {
	return specifierPattern.MatchString(placeholder)
}

func summarizePlaceholders(text string) placeholderSummary {
	summary := placeholderSummary{}
	next := 1
	for _, placeholder := range Placeholders(text) {
		if match := specifierPattern.FindStringSubmatch(placeholder); match != nil {
			argument := formatArgument{Index: next, Type: normalizeConversion(match[2])}
			if match[1] != "" {
				argument.Index, _ = strconv.Atoi(match[1])
				argument.Positional = true
			} else {
				next++
			}
			summary.Arguments = append(summary.Arguments, argument)
			continue
		}
		if strings.HasPrefix(placeholder, `\`) {
			continue
		}
		summary.Tokens = append(summary.Tokens, placeholder)
	}
	return summary
}

func normalizeConversion(conversion string) string {
	if strings.HasSuffix(conversion, "i") {
		return strings.TrimSuffix(conversion, "i") + "d"
	}
	return conversion
}

func (s placeholderSummary) argumentSet() []string {
	seen := map[string]bool{}
	var set []string
	for _, argument := range s.Arguments {
		key := argument.String()
		if !seen[key] {
			seen[key] = true
			set = append(set, key)
		}
	}
	slices.Sort(set)
	return set
}

func (s placeholderSummary) reordered() bool {
	for index := 1; index < len(s.Arguments); index++ {
		if s.Arguments[index].Index < s.Arguments[index-1].Index {
			return true
		}
	}
	return false
}

func (s placeholderSummary) hasNonPositional() bool {
	for _, argument := range s.Arguments {
		if !argument.Positional {
			return true
		}
	}
	return false
}

// ValidateTranslation checks that translation uses the same format arguments
// as source. Arguments may change order only with positional specifiers.
func ValidateTranslation(source, translation string) error {
	expected := summarizePlaceholders(source)
	actual := summarizePlaceholders(translation)

	expectedSet := expected.argumentSet()
	actualSet := actual.argumentSet()
	if !slices.Equal(expectedSet, actualSet) {
		return fmt.Errorf("placeholder mismatch: expected arguments %v, got %v", expectedSet, actualSet)
	}
	if actual.reordered() && actual.hasNonPositional() {
		return fmt.Errorf("reordered arguments must all use positional specifiers such as %%1$@ and %%2$@")
	}

	expectedTokens := slices.Clone(expected.Tokens)
	actualTokens := slices.Clone(actual.Tokens)
	slices.Sort(expectedTokens)
	slices.Sort(actualTokens)
	if !slices.Equal(expectedTokens, actualTokens) {
		return fmt.Errorf("placeholder mismatch: expected tokens %v, got %v", expectedTokens, actualTokens)
	}
	return nil
}
