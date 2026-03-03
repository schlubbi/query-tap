// Package comment defines the CommentParser interface and built-in parsers
// for extracting structured metadata from SQL comments (marginalia, sqlcommenter, etc.).
package comment

import "strings"

// CommentParser extracts structured tags from SQL query comments.
type CommentParser interface {
	// Parse extracts key-value pairs from a SQL comment body.
	// Input is the raw content between /* and */ (without the delimiters).
	// Returns nil or empty map if the comment doesn't match this parser's format.
	Parse(commentBody string) map[string]string
}

// ExtractComment finds the first /* ... */ comment in a SQL query.
// Returns the comment body (without delimiters) and the query with the comment removed.
// If no comment found, returns ("", originalQuery).
func ExtractComment(query string) (commentBody string, strippedQuery string) {
	start := strings.Index(query, "/*")
	if start == -1 {
		return "", query
	}
	end := strings.Index(query[start:], "*/")
	if end == -1 {
		return "", query
	}
	// end is relative to start; absolute position of */ is start+end
	absEnd := start + end

	body := strings.TrimSpace(query[start+2 : absEnd])

	// Build stripped query: everything before the comment + everything after
	before := query[:start]
	after := query[absEnd+2:]

	stripped := strings.TrimSpace(before + after)

	return body, stripped
}
