package wappalyzer

import (
	"strings"
	"testing"
)

func TestNewLoadsDatabase(t *testing.T) {
	w, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(w.fingerprints) < 5000 {
		t.Fatalf("expected thousands of fingerprints, got %d", len(w.fingerprints))
	}
	if w.categories[1] != "CMS" {
		t.Fatalf("expected category 1 == CMS, got %q", w.categories[1])
	}
	if !strings.Contains(w.collectorJS, "querySelector") {
		t.Fatalf("collector JS looks malformed")
	}
}

func has(matches []Match, name string) *Match {
	for i := range matches {
		if matches[i].Name == name {
			return &matches[i]
		}
	}
	return nil
}

func TestHeaderMatchWithVersion(t *testing.T) {
	w, _ := New()
	m := w.Fingerprint(&Input{
		Headers: map[string][]string{"Server": {"nginx/1.25.3"}},
	})
	nginx := has(m, "Nginx")
	if nginx == nil {
		t.Fatalf("Nginx not detected via Server header; got %v", names(m))
	}
	if nginx.Version != "1.25.3" {
		t.Errorf("expected version 1.25.3, got %q", nginx.Version)
	}
}

func TestJSMatch(t *testing.T) {
	w, _ := New()
	m := w.Fingerprint(&Input{
		JS: map[string]string{"jQuery.fn.jquery": "3.6.0"},
	})
	jq := has(m, "jQuery")
	if jq == nil {
		t.Fatalf("jQuery not detected via js; got %v", names(m))
	}
	if jq.Version != "3.6.0" {
		t.Errorf("expected jQuery version 3.6.0, got %q", jq.Version)
	}
}

func TestDOMExistenceMatch(t *testing.T) {
	w, _ := New()
	// React defines DOM selectors like div[id*='react-root'] (existence).
	m := w.Fingerprint(&Input{
		DOM: map[string]DOMNode{"div[id*='react-root']": {Exists: true}},
	})
	if has(m, "React") == nil {
		t.Fatalf("React not detected via DOM existence; got %v", names(m))
	}
}

func TestImpliesResolution(t *testing.T) {
	w, _ := New()
	// WordPress implies PHP + MySQL. Detect WP via its generator meta tag.
	m := w.Fingerprint(&Input{
		Meta: map[string][]string{"generator": {"WordPress 6.5"}},
	})
	if has(m, "WordPress") == nil {
		t.Fatalf("WordPress not detected; got %v", names(m))
	}
	if has(m, "PHP") == nil {
		t.Errorf("expected PHP to be implied by WordPress; got %v", names(m))
	}
}

func TestCollectorScriptWellFormed(t *testing.T) {
	w, _ := New()
	js := w.CollectorJS()
	if strings.Contains(js, "__JS_PATHS__") || strings.Contains(js, "__DOM_SPECS__") {
		t.Fatalf("collector template placeholders not substituted")
	}
}

func TestResolveVersionBackref(t *testing.T) {
	p := parsePattern(`nginx/([\d.]+)\;version:\1`)
	if p == nil {
		t.Fatal("pattern failed to compile")
	}
	ok, ver := p.match("nginx/1.25.3")
	if !ok || ver != "1.25.3" {
		t.Fatalf("backref version: ok=%v ver=%q", ok, ver)
	}
}

func TestResolveVersionTernary(t *testing.T) {
	// \1?a:b -> use "a" when group 1 matched, else "b".
	if got := resolveVersion(`\1?4:3`, []string{"match", "x"}); got != "4" {
		t.Errorf("ternary present: got %q want 4", got)
	}
	if got := resolveVersion(`\1?4:3`, []string{"match", ""}); got != "3" {
		t.Errorf("ternary absent: got %q want 3", got)
	}
}

func TestConfidenceModifier(t *testing.T) {
	p := parsePattern(`something\;confidence:40`)
	if p == nil || p.confidence != 40 {
		t.Fatalf("confidence parse: %+v", p)
	}
	p2 := parsePattern(`something`)
	if p2 == nil || p2.confidence != 100 {
		t.Fatalf("default confidence should be 100: %+v", p2)
	}
}

func TestUncompilablePatternSkipped(t *testing.T) {
	// A backreference in the regex body is not supported by RE2; parsePattern
	// must return nil rather than panic.
	if p := parsePattern(`(a)\1`); p != nil {
		t.Errorf("expected nil for RE2-incompatible pattern, got %+v", p)
	}
}

func TestCollectorExprIsInvoked(t *testing.T) {
	w, _ := New()
	expr := w.CollectorExpr()
	if !strings.HasPrefix(expr, "(") || !strings.HasSuffix(expr, ")()") {
		t.Fatalf("CollectorExpr should be a self-invoked expression")
	}
}

func names(m []Match) []string {
	out := make([]string, 0, len(m))
	for _, x := range m {
		out = append(out, x.Name)
	}
	return out
}
