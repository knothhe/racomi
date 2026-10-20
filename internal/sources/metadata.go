package sources

import (
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func parseFrontmatter(body []byte) (yaml.Node, error) {
	var document yaml.Node
	err := yaml.Unmarshal(body, &document)
	if err == nil {
		return document, nil
	}
	// Some published skills use prose with embedded colons or unindented
	// continuation lines for description. Retry only that plain-text field;
	// all other YAML and the required metadata validation remain strict.
	if normalized, ok := normalizePlainDescription(string(body)); ok {
		var compatible yaml.Node
		if yaml.Unmarshal([]byte(normalized), &compatible) == nil {
			return compatible, nil
		}
	}
	return yaml.Node{}, err
}

func normalizePlainDescription(body string) (string, bool) {
	lines := strings.Split(body, "\n")
	for start, line := range lines {
		key, value, ok := plainFrontmatterField(line)
		if !ok || key != "description" {
			continue
		}
		value = strings.TrimSpace(value)
		if !plainDescriptionText(value) {
			return "", false
		}
		// Preserve the rejection of implicit YAML numbers, booleans and nulls.
		var initial yaml.Node
		if yaml.Unmarshal([]byte(value), &initial) == nil && len(initial.Content) == 1 {
			node := initial.Content[0]
			if node.Kind == yaml.ScalarNode && node.Tag != "!!str" {
				return "", false
			}
		}
		parts := []string{value}
		end := start + 1
		for ; end < len(lines); end++ {
			if _, _, field := plainFrontmatterField(lines[end]); field {
				break
			}
			text := strings.TrimSpace(lines[end])
			if text == "" || strings.HasPrefix(text, "#") {
				continue
			}
			if !plainDescriptionText(text) {
				return "", false
			}
			// An indented mapping is structured YAML, not prose continuation.
			if _, _, field := plainFrontmatterField(text); field {
				return "", false
			}
			parts = append(parts, text)
		}
		lines[start] = "description: " + strconv.Quote(strings.Join(parts, " "))
		return strings.Join(append(lines[:start+1], lines[end:]...), "\n"), true
	}
	return "", false
}

// Recognize simple top-level field names without treating prose such as
// "Also for app screens: dashboards" as another metadata field.
func plainFrontmatterField(line string) (string, string, bool) {
	colon := strings.IndexByte(line, ':')
	if colon <= 0 || colon+1 < len(line) && line[colon+1] != ' ' && line[colon+1] != '\t' {
		return "", "", false
	}
	key := strings.TrimRight(line[:colon], " \t")
	for i, r := range key {
		if r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (i == 0 || r != '-' && (r < '0' || r > '9')) {
			return "", "", false
		}
	}
	return key, line[colon+1:], true
}

func plainDescriptionText(text string) bool {
	if text == "" || strings.ContainsRune("\"'[{]}|>&*!%@`#", rune(text[0])) {
		return false
	}
	return !strings.HasPrefix(text, "- ") && !strings.HasPrefix(text, "? ") && !strings.HasPrefix(text, ": ")
}
