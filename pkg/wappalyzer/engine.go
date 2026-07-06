package wappalyzer

import (
	"encoding/json"
	"strings"
)

// rawTech mirrors the on-disk Wappalyzer fingerprint schema. Fields whose value
// may be either a string, an array of strings or an object are kept as
// json.RawMessage and decoded field-by-field.
type rawTech struct {
	Cats             []int                      `json:"cats"`
	Icon             string                     `json:"icon"`
	Headers          map[string]json.RawMessage `json:"headers"`
	Cookies          map[string]json.RawMessage `json:"cookies"`
	Meta             map[string]json.RawMessage `json:"meta"`
	JS               map[string]json.RawMessage `json:"js"`
	HTML             json.RawMessage            `json:"html"`
	URL              json.RawMessage            `json:"url"`
	ScriptSrc        json.RawMessage            `json:"scriptSrc"`
	DOM              json.RawMessage            `json:"dom"`
	Implies          json.RawMessage            `json:"implies"`
	Requires         json.RawMessage            `json:"requires"`
	RequiresCategory json.RawMessage            `json:"requiresCategory"`
	Excludes         json.RawMessage            `json:"excludes"`
}

// domSpec describes what to check on the element(s) matched by a DOM selector.
type domSpec struct {
	exists     bool
	text       []*pattern
	attributes map[string][]*pattern
	properties map[string][]*pattern
}

// implication is a resolved `implies` entry: a technology name plus the version
// and confidence to attribute to it when the implying technology is detected.
type implication struct {
	name       string
	version    string
	confidence int
}

// fingerprint is a compiled technology definition ready for matching.
type fingerprint struct {
	name        string
	cats        []int
	icon        string
	headers     map[string][]*pattern // header name (lower-cased)
	cookies     map[string][]*pattern // cookie name (lower-cased)
	meta        map[string][]*pattern // meta name (lower-cased)
	js          map[string][]*pattern // js property path (original case)
	html        []*pattern
	url         []*pattern
	scriptSrc   []*pattern
	domExists   []string           // array-form selectors: existence only
	domSpecs    map[string]domSpec // object-form selectors
	implies     []implication
	requires    []string
	requiresCat []int
	excludes    []string
}

// compile turns a raw fingerprint into its matchable form.
func compile(name string, rt *rawTech) *fingerprint {
	fp := &fingerprint{
		name: name,
		cats: rt.Cats,
		icon: rt.Icon,
	}

	fp.headers = compileNamedPatterns(rt.Headers)
	fp.cookies = compileNamedPatterns(rt.Cookies)
	fp.meta = compileNamedPatterns(rt.Meta)
	fp.js = compileNamedPatternsRaw(rt.JS) // keep original-case js paths

	fp.html = compilePatternList(rt.HTML)
	fp.url = compilePatternList(rt.URL)
	fp.scriptSrc = compilePatternList(rt.ScriptSrc)

	fp.compileDOM(rt.DOM)
	fp.compileImplies(rt.Implies)

	fp.requires = rawStrings(rt.Requires)
	fp.requiresCat = rawInts(rt.RequiresCategory)
	fp.excludes = rawStrings(rt.Excludes)

	return fp
}

// compileNamedPatterns compiles a {name: pattern|[patterns]} object, lowercasing
// the keys for case-insensitive lookup.
func compileNamedPatterns(m map[string]json.RawMessage) map[string][]*pattern {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string][]*pattern, len(m))
	for k, v := range m {
		if ps := compilePatternList(v); len(ps) > 0 {
			out[strings.ToLower(k)] = ps
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// compileNamedPatternsRaw is like compileNamedPatterns but preserves key case
// (used for js property paths, which are case-sensitive).
func compileNamedPatternsRaw(m map[string]json.RawMessage) map[string][]*pattern {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string][]*pattern, len(m))
	for k, v := range m {
		if ps := compilePatternList(v); len(ps) > 0 {
			out[k] = ps
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// compilePatternList compiles a raw value that is either a JSON string or an
// array of JSON strings into a slice of compiled patterns.
func compilePatternList(raw json.RawMessage) []*pattern {
	var out []*pattern
	for _, s := range rawStrings(raw) {
		if p := parsePattern(s); p != nil {
			out = append(out, p)
		}
	}
	return out
}

func (fp *fingerprint) compileDOM(raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}

	// Array form: list of selectors, existence-only.
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		fp.domExists = arr
		return
	}

	// Object form: {selector: {exists, text, attributes, properties}}.
	var obj map[string]struct {
		Exists     json.RawMessage            `json:"exists"`
		Text       json.RawMessage            `json:"text"`
		Attributes map[string]json.RawMessage `json:"attributes"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return
	}

	fp.domSpecs = make(map[string]domSpec, len(obj))
	for sel, spec := range obj {
		ds := domSpec{
			exists: spec.Exists != nil,
			text:   compilePatternList(spec.Text),
		}
		if len(spec.Attributes) > 0 {
			ds.attributes = compileNamedPatterns(spec.Attributes)
		}
		if len(spec.Properties) > 0 {
			ds.properties = compileNamedPatternsRaw(spec.Properties)
		}
		fp.domSpecs[sel] = ds
	}
}

func (fp *fingerprint) compileImplies(raw json.RawMessage) {
	for _, s := range rawStrings(raw) {
		parts := strings.Split(s, `\;`)
		imp := implication{name: parts[0], confidence: 100}
		for _, mod := range parts[1:] {
			kv := strings.SplitN(mod, ":", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case "version":
				imp.version = kv[1]
			case "confidence":
				if n := atoiSafe(kv[1]); n >= 0 {
					imp.confidence = n
				}
			}
		}
		fp.implies = append(fp.implies, imp)
	}
}

// rawStrings decodes a raw value that is a JSON string, an array of strings, or
// an array of mixed scalars into a []string.
func rawStrings(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []string{s}
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	return nil
}

// rawInts decodes a raw value that is a JSON number or an array of numbers.
func rawInts(raw json.RawMessage) []int {
	if len(raw) == 0 {
		return nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return []int{n}
	}
	var arr []int
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	return nil
}

func atoiSafe(s string) int {
	n := 0
	s = strings.TrimSpace(s)
	if s == "" {
		return -1
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}
