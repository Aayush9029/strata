package sourcecontext

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Aayush9029/strata/internal/catalog"
)

type Index struct {
	occurrences map[string][]catalog.UsageContext
}

type astGrepMatch struct {
	Text          string    `json:"text"`
	File          string    `json:"file"`
	Lines         string    `json:"lines"`
	Range         nodeRange `json:"range"`
	MetaVariables struct {
		Single map[string]struct {
			Text string `json:"text"`
		} `json:"single"`
	} `json:"metaVariables"`
}

type nodeRange struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

type position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type declaration struct {
	File  string
	Name  string
	Kind  string
	Range nodeRange
}

func Build(roots []string) (*Index, error) {
	if len(roots) == 0 {
		roots = []string{"."}
	}
	if _, err := exec.LookPath("ast-grep"); err != nil {
		return nil, errors.New("smart_context requires ast-grep; install it or set smart_context to false")
	}

	strings, err := runAstGrep(`"$TEXT"`, roots)
	if err != nil {
		return nil, err
	}
	declarations, err := findDeclarations(roots)
	if err != nil {
		return nil, err
	}

	index := &Index{occurrences: map[string][]catalog.UsageContext{}}
	for _, match := range strings {
		value := match.MetaVariables.Single["TEXT"].Text
		if value == "" {
			continue
		}
		context := contextFor(match, declarations)
		index.occurrences[value] = append(index.occurrences[value], context)
	}
	return index, nil
}

func (i *Index) Attach(items []catalog.Item, limit int) []catalog.Item {
	if i == nil || limit == 0 {
		return items
	}
	for index := range items {
		items[index].UsageContext = i.Find(items[index].Key, items[index].Source, limit)
	}
	return items
}

func (i *Index) Find(key, source string, limit int) []catalog.UsageContext {
	needles := compactNeedles(key, source)
	contexts := []catalog.UsageContext{}
	for _, needle := range needles {
		for _, context := range i.occurrences[needle] {
			contexts = appendUniqueContext(contexts, context)
			if limit > 0 && len(contexts) >= limit {
				return contexts
			}
		}
	}
	return contexts
}

func findDeclarations(roots []string) ([]declaration, error) {
	patterns := []struct {
		Kind    string
		Pattern string
	}{
		{Kind: "struct", Pattern: `struct $NAME { $$$ }`},
		{Kind: "class", Pattern: `class $NAME { $$$ }`},
		{Kind: "actor", Pattern: `actor $NAME { $$$ }`},
		{Kind: "enum", Pattern: `enum $NAME { $$$ }`},
		{Kind: "func", Pattern: `func $NAME($$$) { $$$ }`},
		{Kind: "var", Pattern: `var $NAME: $$$`},
	}

	var declarations []declaration
	for _, pattern := range patterns {
		matches, err := runAstGrep(pattern.Pattern, roots)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			name := match.MetaVariables.Single["NAME"].Text
			if name == "" {
				continue
			}
			declarations = append(declarations, declaration{
				File:  match.File,
				Name:  name,
				Kind:  pattern.Kind,
				Range: match.Range,
			})
		}
	}
	return declarations, nil
}

func contextFor(match astGrepMatch, declarations []declaration) catalog.UsageContext {
	context := catalog.UsageContext{
		File:    match.File,
		Line:    match.Range.Start.Line,
		Snippet: strings.TrimSpace(match.Lines),
	}
	for _, declaration := range declarations {
		if declaration.File != match.File || !containsLine(declaration.Range, match.Range.Start.Line) {
			continue
		}
		switch declaration.Kind {
		case "struct", "class", "actor", "enum":
			if context.Type == "" || lineSpan(declaration.Range) < lineSpanForContext(context, declarations) {
				context.Type = declaration.Name
				if isViewSnippet(context.Snippet) {
					context.View = declaration.Name
				}
			}
		case "func", "var":
			context.Function = declaration.Name
		}
	}
	return context
}

func runAstGrep(pattern string, roots []string) ([]astGrepMatch, error) {
	args := []string{"run", "--lang", "swift", "--pattern", pattern, "--json=stream"}
	args = append(args, roots...)
	command := exec.Command("ast-grep", args...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		if stdout.Len() == 0 {
			return nil, nil
		}
		if strings.Contains(stderr.String(), "No files found") {
			return nil, nil
		}
		return nil, fmt.Errorf("ast-grep %q failed: %w: %s", pattern, err, strings.TrimSpace(stderr.String()))
	}

	var matches []astGrepMatch
	scanner := bufio.NewScanner(bytes.NewReader(stdout.Bytes()))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var match astGrepMatch
		if err := json.Unmarshal([]byte(line), &match); err != nil {
			return nil, fmt.Errorf("decode ast-grep JSON: %w", err)
		}
		matches = append(matches, match)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return matches, nil
}

func containsLine(node nodeRange, line int) bool {
	return node.Start.Line <= line && line <= node.End.Line
}

func lineSpan(node nodeRange) int {
	return node.End.Line - node.Start.Line
}

func lineSpanForContext(context catalog.UsageContext, declarations []declaration) int {
	if context.Type == "" {
		return int(^uint(0) >> 1)
	}
	for _, declaration := range declarations {
		if declaration.Name == context.Type {
			return lineSpan(declaration.Range)
		}
	}
	return int(^uint(0) >> 1)
}

func isViewSnippet(line string) bool {
	return strings.Contains(line, "Text(") || strings.Contains(line, "String(localized:") || strings.Contains(line, "LocalizedStringKey") || strings.Contains(line, "Label(")
}

func compactNeedles(values ...string) []string {
	seen := map[string]bool{}
	var needles []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len([]rune(value)) < 2 || seen[value] {
			continue
		}
		seen[value] = true
		needles = append(needles, value)
	}
	return needles
}

func appendUniqueContext(values []catalog.UsageContext, value catalog.UsageContext) []catalog.UsageContext {
	for _, existing := range values {
		if existing.File == value.File && existing.Line == value.Line && existing.Snippet == value.Snippet {
			return values
		}
	}
	return append(values, value)
}
