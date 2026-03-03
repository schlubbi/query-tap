package comment

import "strings"

// MarginaliaParser parses marginalia-style SQL comments with comma-separated
// key=value pairs (e.g., "app=web,controller=users").
type MarginaliaParser struct{}

// Parse extracts key-value pairs from a comma-separated "key=value" comment body.
func (p *MarginaliaParser) Parse(commentBody string) map[string]string {
	result := make(map[string]string)
	if strings.TrimSpace(commentBody) == "" {
		return result
	}
	pairs := strings.Split(commentBody, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		result[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return result
}
