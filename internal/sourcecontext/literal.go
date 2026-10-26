package sourcecontext

import (
	"strconv"
	"strings"

	"github.com/Aayush9029/strata/internal/catalog"
)

// interpolationToken stands in for a Swift interpolation or a format specifier
// so that "\(size) available" and "%@ available" share one lookup key.
const interpolationToken = "\x00"

type swiftLiteral struct {
	Normalized     string
	Interpolations []string
}

// parseSwiftLiteral decodes a single-line Swift string literal, including its
// quotes. Each interpolation becomes interpolationToken.
func parseSwiftLiteral(text string) swiftLiteral {
	literal := swiftLiteral{}
	if len(text) < 2 || text[0] != '"' || text[len(text)-1] != '"' {
		return literal
	}
	runes := []rune(text[1 : len(text)-1])
	var builder strings.Builder
	for index := 0; index < len(runes); index++ {
		char := runes[index]
		if char != '\\' || index+1 >= len(runes) {
			builder.WriteRune(char)
			continue
		}
		index++
		switch runes[index] {
		case '(':
			end := closingParen(runes, index)
			literal.Interpolations = append(literal.Interpolations, strings.TrimSpace(string(runes[index+1:end])))
			builder.WriteString(interpolationToken)
			index = end
		case 'n':
			builder.WriteRune('\n')
		case 't':
			builder.WriteRune('\t')
		case 'r':
			builder.WriteRune('\r')
		case '0':
			builder.WriteRune(0)
		case 'u':
			if index+1 < len(runes) && runes[index+1] == '{' {
				if end := indexRune(runes, index+1, '}'); end > 0 {
					if code, err := strconv.ParseInt(string(runes[index+2:end]), 16, 32); err == nil {
						builder.WriteRune(rune(code))
						index = end
						continue
					}
				}
			}
			builder.WriteRune('u')
		default:
			builder.WriteRune(runes[index])
		}
	}
	literal.Normalized = builder.String()
	return literal
}

func closingParen(runes []rune, open int) int {
	depth := 0
	inString := false
	for index := open; index < len(runes); index++ {
		switch char := runes[index]; {
		case char == '\\' && inString:
			index++
		case char == '"':
			inString = !inString
		case inString:
		case char == '(':
			depth++
		case char == ')':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return len(runes) - 1
}

func indexRune(runes []rune, start int, target rune) int {
	for index := start; index < len(runes); index++ {
		if runes[index] == target {
			return index
		}
	}
	return -1
}

// NormalizeFormat replaces format specifiers in a catalog key with the token
// used for Swift interpolations, and %% with %.
func NormalizeFormat(text string) string {
	return catalog.PlaceholderPattern.ReplaceAllStringFunc(text, func(match string) string {
		switch {
		case match == "%%":
			return "%"
		case catalog.IsFormatSpecifier(match):
			return interpolationToken
		default:
			return match
		}
	})
}

var roleMarkers = []struct {
	Marker string
	Role   string
}{
	{"Button(", "button"},
	{"Toggle(", "toggle"},
	{"Picker(", "picker"},
	{"Stepper(", "stepper"},
	{"Menu(", "menu"},
	{"Label(", "label"},
	{"LabeledContent(", "label"},
	{"Link(", "link"},
	{"NavigationLink(", "navigation link"},
	{"ShareLink(", "share button"},
	{"TextField(", "text field placeholder"},
	{"SecureField(", "text field placeholder"},
	{"Section(", "section header"},
	{"Tab(", "tab"},
	{"Text(", "text"},
	{"ContentUnavailableView(", "empty state"},
	{".navigationTitle(", "navigation title"},
	{".navigationSubtitle(", "navigation subtitle"},
	{".alert(", "alert title"},
	{".confirmationDialog(", "confirmation dialog title"},
	{".help(", "tooltip"},
	{".accessibilityLabel(", "accessibility label"},
	{".accessibilityHint(", "accessibility hint"},
	{".searchable(", "search field prompt"},
	{".badge(", "badge"},
}

// roleFor guesses the UI role of a string from the call that encloses it on
// its line, such as Button( or .navigationTitle(.
func roleFor(lines string, column int) string {
	line, _, _ := strings.Cut(lines, "\n")
	runes := []rune(line)
	if column >= 0 && column < len(runes) {
		runes = runes[:column]
	}
	prefix := string(runes)
	role := ""
	bestEnd := -1
	bestLength := 0
	for _, marker := range roleMarkers {
		index := strings.LastIndex(prefix, marker.Marker)
		if index < 0 {
			continue
		}
		end := index + len(marker.Marker)
		if end > bestEnd || (end == bestEnd && len(marker.Marker) > bestLength) {
			role = marker.Role
			bestEnd = end
			bestLength = len(marker.Marker)
		}
	}
	return role
}
