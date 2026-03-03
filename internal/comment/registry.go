package comment

import "fmt"

var registry = map[string]CommentParser{
	"marginalia": &MarginaliaParser{},
	"rails":      &RailsStyleParser{},
}

// Get returns a parser by name. Known names: "marginalia", "rails".
func Get(name string) (CommentParser, error) {
	p, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown comment parser: %q", name)
	}
	return p, nil
}

// Register adds a custom parser to the registry.
func Register(name string, parser CommentParser) {
	registry[name] = parser
}
