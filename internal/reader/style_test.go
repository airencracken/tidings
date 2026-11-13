// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"fmt"
	"regexp"
	"strconv"
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

var cssPixelSize = regexp.MustCompile(`(?:font-size:\s*|font:[^;]*?\b)(\d+(?:\.\d+)?)px`)

// smallType lists declared text sizes under the 12px reading floor. Zero is
// allowed: it hides a visible label while keeping its accessible name.
func smallType(source string) []string {
	var found []string
	for n, line := range strings.Split(source, "\n") {
		for _, match := range cssPixelSize.FindAllStringSubmatch(line, -1) {
			size, err := strconv.ParseFloat(match[1], 64)
			if err == nil && size > 0 && size < 12 {
				found = append(found, fmt.Sprintf("line %d: %s", n+1, match[0]))
			}
		}
	}
	return found
}

func TestStylesheetKeepsTextAtLeastTwelvePixels(t *testing.T) {
	source := appStylesheet(t)
	for _, problem := range smallType(source) {
		t.Error(problem)
	}
	for _, bad := range []string{".a { font-size: 11px; }", ".a { font: 400 10px/1.5 serif; }", ".a { font: italic 11.5px Georgia; }"} {
		if len(smallType(bad)) != 1 {
			t.Errorf("small type not detected in %q", bad)
		}
	}
	for _, ok := range []string{".a { font-size: 12px; }", ".a { font: 400 14px/1.5 serif; padding: 2px 8px; }", ".a { font-size: 0; }"} {
		if found := smallType(ok); len(found) != 0 {
			t.Errorf("false positive in %q: %v", ok, found)
		}
	}
	// Actionable controls get 13-14px; the article body keeps its 19px Georgia.
	for _, want := range []string{
		".story-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 20px; font-size: 14px; }",
		".text-button { padding: 2px 0; border: 0; background: transparent; color: var(--muted); font-size: 14px;",
		".reader-body { font: 19px/1.8 Georgia,",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("stylesheet lost %q", want)
		}
	}
}

func TestTidingsMapsItsColoursToTheSharedTokens(t *testing.T) {
	source := appStylesheet(t)
	for _, want := range []string{
		"--comfy-surface: light-dark(#f5f3ea, #171f1a);", "--comfy-raised:", "--comfy-border:", "--comfy-control-border:",
		"--comfy-text:", "--comfy-muted:", "--comfy-accent:", "--comfy-accent-text:", "--comfy-danger:",
		`--comfy-heading-font: Georgia, "Times New Roman", serif;`, "--comfy-heading-weight: 400;",
		"--ink: var(--comfy-text);", "--paper: var(--comfy-raised);", "--green: var(--comfy-accent);",
		"h1, h2 { font-family: var(--comfy-heading-font);",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("stylesheet is missing %q", want)
		}
	}
	// Overrides of shared pages and the old token names are gone: the shared
	// styles read the tokens directly.
	for _, gone := range []string{"--comfy-ink", "--comfy-panel", "--comfy-line", "--comfy-admin-muted", `[data-comfy-app="tidings"] .comfy-admin-dashboard`, `:is(.account-page, .invitation-page)`, ".theme-picker[hidden]"} {
		if strings.Contains(source, gone) {
			t.Errorf("stylesheet still has %q", gone)
		}
	}
	// Tidings' own names must never be read back by the tokens, or the
	// shared fallback would loop.
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "--comfy-") && strings.Contains(line, "var(--") && !strings.Contains(line, "var(--comfy-") {
			t.Errorf("token reads an app variable: %s", line)
		}
	}
	page := webHarness(t).request(1, "GET", "/account/invitations", nil).Body.String()
	for _, want := range []string{`data-comfy-app="tidings"`, `data-palette="sage"`, `href="/static/comfyware/palettes.css"`} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing %q", want)
		}
	}
}
