// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"fmt"
	"strings"
	"testing"
)

type cssRule struct {
	block, selector, body string
	line                  int
}

// parseStylesheet reads the app stylesheet's one-rule-per-line layout. It
// rejects lines carrying several rules, so hand-minified patches cannot return.
func parseStylesheet(source string) ([]cssRule, error) {
	var rules []cssRule
	block := ""
	comment := false
	for n, raw := range strings.Split(source, "\n") {
		line := strings.TrimSpace(raw)
		if comment || strings.HasPrefix(line, "/*") {
			comment = !strings.Contains(line, "*/")
			continue
		}
		switch {
		case line == "":
		case strings.HasPrefix(line, "@media"):
			if block != "" || !strings.HasSuffix(line, "{") {
				return nil, fmt.Errorf("line %d: nested or malformed media block", n+1)
			}
			block = strings.TrimSpace(strings.TrimSuffix(line, "{"))
		case line == "}":
			switch {
			case strings.HasPrefix(block, "rule:"):
				block = strings.TrimPrefix(block, "rule:")
			case block == "":
				return nil, fmt.Errorf("line %d: unmatched closing brace", n+1)
			default:
				block = ""
			}
		case strings.Count(line, "{") == 1 && strings.HasSuffix(line, "}") && strings.Count(line, "}") == 1:
			open := strings.Index(line, "{")
			rules = append(rules, cssRule{block: block, selector: strings.TrimSpace(line[:open]), body: line[open+1 : len(line)-1], line: n + 1})
		case strings.HasSuffix(line, "{"):
			// A multi-line rule, such as :root, keeps its declarations below.
			open := strings.Index(line, "{")
			rules = append(rules, cssRule{block: block, selector: strings.TrimSpace(line[:open]), line: n + 1})
			block = "rule:" + block
		default:
			if strings.HasPrefix(block, "rule:") && strings.HasSuffix(line, ";") && !strings.ContainsAny(line, "{}") {
				rules[len(rules)-1].body += " " + line
				continue
			}
			return nil, fmt.Errorf("line %d: expected one rule per line, got %q", n+1, line)
		}
	}
	if block != "" {
		return nil, fmt.Errorf("unterminated block %q", block)
	}
	return rules, nil
}

func appStylesheet(t *testing.T) string {
	t.Helper()
	data, err := assets.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// stylesheetProblems reports overrides split from their rule and repeated
// breakpoints, the two patterns that made the old stylesheet hard to edit.
func stylesheetProblems(source string) []string {
	rules, err := parseStylesheet(source)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	seen := map[string]int{}
	for _, rule := range rules {
		key := rule.block + "\x00" + rule.selector
		if first, ok := seen[key]; ok {
			problems = append(problems, fmt.Sprintf("%q in %q is declared on lines %d and %d", rule.selector, rule.block, first, rule.line))
		}
		seen[key] = rule.line
	}
	media := map[string]int{}
	for _, line := range strings.Split(source, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "@media") {
			media[strings.TrimSpace(line)]++
		}
	}
	for query, count := range media {
		if count != 1 {
			problems = append(problems, fmt.Sprintf("%s appears %d times", query, count))
		}
	}
	return problems
}

func TestStylesheetHasOneRulePerSelectorAndBreakpoint(t *testing.T) {
	source := appStylesheet(t)
	rules, err := parseStylesheet(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) < 100 {
		t.Fatalf("parsed only %d rules; the stylesheet layout changed", len(rules))
	}
	for _, problem := range stylesheetProblems(source) {
		t.Error(problem)
	}
}

func TestStylesheetParserRejectsMinifiedAndPatchedRules(t *testing.T) {
	for name, source := range map[string]string{
		"two rules on one line": ".a { color: red; } .b { color: blue; }\n",
		"unclosed media":        "@media (max-width: 760px) {\n.a { color: red; }\n",
		"nested media":          "@media (max-width: 760px) {\n@media (max-width: 400px) {\n}\n}\n",
		"stray declaration":     "color: red;\n",
		"split override":        ".a { color: red; }\n.b { color: blue; }\n.a { margin: 0; }\n",
		"repeated breakpoint":   "@media (max-width: 760px) {\n.a { color: red; }\n}\n@media (max-width: 760px) {\n.b { color: red; }\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if len(stylesheetProblems(source)) == 0 {
				t.Fatalf("parser accepted %q", source)
			}
		})
	}
}
