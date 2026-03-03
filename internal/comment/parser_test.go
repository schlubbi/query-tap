package comment

import (
	"testing"
)

// --- ExtractComment tests ---

func TestExtractComment_LeadingComment(t *testing.T) {
	body, stripped := ExtractComment("/* app=web */ SELECT 1")
	if body != "app=web" {
		t.Errorf("body = %q, want %q", body, "app=web")
	}
	if stripped != "SELECT 1" {
		t.Errorf("stripped = %q, want %q", stripped, "SELECT 1")
	}
}

func TestExtractComment_MidQueryComment(t *testing.T) {
	body, stripped := ExtractComment("SELECT /* app=web */ 1")
	if body != "app=web" {
		t.Errorf("body = %q, want %q", body, "app=web")
	}
	if stripped != "SELECT  1" {
		t.Errorf("stripped = %q, want %q", stripped, "SELECT  1")
	}
}

func TestExtractComment_NoComment(t *testing.T) {
	body, stripped := ExtractComment("SELECT 1")
	if body != "" {
		t.Errorf("body = %q, want %q", body, "")
	}
	if stripped != "SELECT 1" {
		t.Errorf("stripped = %q, want %q", stripped, "SELECT 1")
	}
}

func TestExtractComment_EmptyComment(t *testing.T) {
	body, stripped := ExtractComment("/* */ SELECT 1")
	if body != "" {
		t.Errorf("body = %q, want %q", body, "")
	}
	if stripped != "SELECT 1" {
		t.Errorf("stripped = %q, want %q", stripped, "SELECT 1")
	}
}

func TestExtractComment_Multiline(t *testing.T) {
	body, stripped := ExtractComment("/* multi line\ncomment */ SELECT 1")
	if body != "multi line\ncomment" {
		t.Errorf("body = %q, want %q", body, "multi line\ncomment")
	}
	if stripped != "SELECT 1" {
		t.Errorf("stripped = %q, want %q", stripped, "SELECT 1")
	}
}

// --- MarginaliaParser tests ---

func TestMarginaliaParser_MultiplePairs(t *testing.T) {
	p := &MarginaliaParser{}
	got := p.Parse("app=web,controller=users")
	assertMap(t, got, map[string]string{"app": "web", "controller": "users"})
}

func TestMarginaliaParser_SinglePair(t *testing.T) {
	p := &MarginaliaParser{}
	got := p.Parse("key=value")
	assertMap(t, got, map[string]string{"key": "value"})
}

func TestMarginaliaParser_Empty(t *testing.T) {
	p := &MarginaliaParser{}
	got := p.Parse("")
	assertMap(t, got, map[string]string{})
}

func TestMarginaliaParser_EmptyValue(t *testing.T) {
	p := &MarginaliaParser{}
	got := p.Parse("key=")
	assertMap(t, got, map[string]string{"key": ""})
}

func TestMarginaliaParser_ValueContainsEquals(t *testing.T) {
	p := &MarginaliaParser{}
	got := p.Parse("key=val=ue")
	assertMap(t, got, map[string]string{"key": "val=ue"})
}

func TestMarginaliaParser_Whitespace(t *testing.T) {
	p := &MarginaliaParser{}
	got := p.Parse(" app = web , controller = users ")
	assertMap(t, got, map[string]string{"app": "web", "controller": "users"})
}

// --- RailsStyleParser tests ---

func TestRailsStyleParser_MultiplePairs(t *testing.T) {
	p := &RailsStyleParser{}
	got := p.Parse("controller:users action:show")
	assertMap(t, got, map[string]string{"controller": "users", "action": "show"})
}

func TestRailsStyleParser_SinglePair(t *testing.T) {
	p := &RailsStyleParser{}
	got := p.Parse("application:myapp")
	assertMap(t, got, map[string]string{"application": "myapp"})
}

func TestRailsStyleParser_Empty(t *testing.T) {
	p := &RailsStyleParser{}
	got := p.Parse("")
	assertMap(t, got, map[string]string{})
}

func TestRailsStyleParser_MultipleSpaces(t *testing.T) {
	p := &RailsStyleParser{}
	got := p.Parse("controller:users  action:show")
	assertMap(t, got, map[string]string{"controller": "users", "action": "show"})
}

func TestRailsStyleParser_NoValidPairs(t *testing.T) {
	p := &RailsStyleParser{}
	got := p.Parse("no-colon-here")
	assertMap(t, got, map[string]string{})
}

// --- Registry tests ---

func TestRegistryGet_Marginalia(t *testing.T) {
	p, err := Get("marginalia")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*MarginaliaParser); !ok {
		t.Errorf("expected *MarginaliaParser, got %T", p)
	}
}

func TestRegistryGet_Rails(t *testing.T) {
	p, err := Get("rails")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*RailsStyleParser); !ok {
		t.Errorf("expected *RailsStyleParser, got %T", p)
	}
}

func TestRegistryGet_Unknown(t *testing.T) {
	_, err := Get("unknown")
	if err == nil {
		t.Fatal("expected error for unknown parser, got nil")
	}
}

// --- test helper ---

func assertMap(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("map length = %d, want %d; got %v", len(got), len(want), got)
		return
	}
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Errorf("missing key %q", k)
			continue
		}
		if gv != wv {
			t.Errorf("key %q = %q, want %q", k, gv, wv)
		}
	}
}
