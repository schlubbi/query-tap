package comment

import "strings"

// RailsStyleParser parses Rails-style SQL comments with space-separated
// key:value pairs (e.g., "controller:users action:show").
type RailsStyleParser struct{}

// Parse extracts key-value pairs from a space-separated "key:value" comment body.
func (p *RailsStyleParser) Parse(commentBody string) map[string]string {
	result := make(map[string]string)
	if strings.TrimSpace(commentBody) == "" {
		return result
	}
	fields := strings.Fields(commentBody)
	for _, field := range fields {
		key, value, ok := strings.Cut(field, ":")
		if !ok {
			continue
		}
		result[key] = value
	}
	return result
}
