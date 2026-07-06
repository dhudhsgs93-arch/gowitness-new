// Package wappalyzer is a native-Go technology fingerprinting engine that
// consumes the upstream Wappalyzer (webappanalyzer) fingerprint database and
// matches it against signals collected from a live headless browser session:
// HTTP headers, cookies, the rendered HTML, script sources, meta tags, in-page
// JavaScript globals and DOM nodes. Running the js/dom checks against the page
// gowitness has already loaded gives detection parity with the Wappalyzer
// browser extension (and s0md3v/wappalyzer-next) without spawning a second
// browser or a Python runtime.
package wappalyzer

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

//go:embed data/technologies.json
var technologiesJSON []byte

//go:embed data/categories.json
var categoriesJSON []byte

// Wappalyzer is a compiled, ready-to-match fingerprint set. It is safe for
// concurrent use by multiple goroutines after New returns.
type Wappalyzer struct {
	fingerprints  []*fingerprint
	byName        map[string]*fingerprint
	categories    map[int]string
	collectorJS   string // function expression (for go-rod page.Eval)
	collectorExpr string // self-invoked expression (for chromedp Evaluate)
	icons         map[string]string // technology name -> icon file
}

// DOMNode is the data extracted from a single DOM element that a fingerprint
// selector matched. It is produced by the in-browser collector and consumed by
// the matching engine.
type DOMNode struct {
	Exists     bool              `json:"exists"`
	Text       string            `json:"text,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

// Input carries every signal the engine can match against. Headers/Cookies/Meta
// names are matched case-insensitively; ScriptSrc/JS/DOM are typically supplied
// by the in-browser collector (see CollectorJS / BrowserResult).
type Input struct {
	URL       string
	Headers   map[string][]string
	Cookies   map[string]string
	HTML      string
	ScriptSrc []string
	Meta      map[string][]string
	JS        map[string]string
	DOM       map[string]DOMNode
}

// Match is a detected technology.
type Match struct {
	Name       string   `json:"name"`
	Version    string   `json:"version,omitempty"`
	Confidence int      `json:"confidence"`
	Categories []string `json:"categories,omitempty"`
}

// New loads and compiles the embedded fingerprint database.
func New() (*Wappalyzer, error) {
	var raw map[string]*rawTech
	if err := json.Unmarshal(technologiesJSON, &raw); err != nil {
		return nil, fmt.Errorf("parsing technologies: %w", err)
	}

	var cats map[string]struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(categoriesJSON, &cats); err != nil {
		return nil, fmt.Errorf("parsing categories: %w", err)
	}

	w := &Wappalyzer{
		byName:     make(map[string]*fingerprint, len(raw)),
		categories: make(map[int]string, len(cats)),
		icons:      make(map[string]string, len(raw)),
	}

	for id, c := range cats {
		if n, err := strconv.Atoi(id); err == nil {
			w.categories[n] = c.Name
		}
	}

	for name, rt := range raw {
		fp := compile(name, rt)
		w.fingerprints = append(w.fingerprints, fp)
		w.byName[name] = fp
		if rt.Icon != "" {
			w.icons[name] = rt.Icon
		}
	}

	w.collectorJS = w.buildCollectorJS()
	w.collectorExpr = "(" + w.collectorJS + ")()"
	return w, nil
}

// Icons returns a technology-name -> icon-file map (for building icon URLs).
func (w *Wappalyzer) Icons() map[string]string { return w.icons }

// IconFile returns the icon filename for a technology name, or "" if it has no
// icon.
func (w *Wappalyzer) IconFile(name string) string { return w.icons[name] }

// detection accumulates evidence for one technology during a Fingerprint pass.
type detection struct {
	fp         *fingerprint
	version    string
	confidence int
}

// Fingerprint runs the full detection pipeline against the given signals and
// returns the detected technologies sorted by confidence (desc), then name.
func (w *Wappalyzer) Fingerprint(in *Input) []Match {
	n := normalize(in)
	det := make(map[string]*detection)

	for _, fp := range w.fingerprints {
		if ok, ver, conf := fp.matchAll(n); ok {
			w.record(det, fp.name, fp, ver, conf)
		}
	}

	w.resolveImplies(det)
	w.applyRequires(det)
	w.applyExcludes(det)

	return w.buildMatches(det)
}

func (w *Wappalyzer) record(det map[string]*detection, name string, fp *fingerprint, ver string, conf int) {
	d, ok := det[name]
	if !ok {
		d = &detection{fp: fp}
		det[name] = d
	}
	d.confidence += conf
	if d.confidence > 100 {
		d.confidence = 100
	}
	d.version = pickVersion(d.version, ver)
}

// resolveImplies expands the `implies` graph to a fixed point. Implied
// technologies inherit a confidence scaled by the implying technology's.
func (w *Wappalyzer) resolveImplies(det map[string]*detection) {
	for changed := true; changed; {
		changed = false
		for _, d := range snapshot(det) {
			if d.fp == nil {
				continue
			}
			for _, imp := range d.fp.implies {
				if _, exists := det[imp.name]; exists {
					continue
				}
				conf := imp.confidence * d.confidence / 100
				if conf <= 0 {
					continue
				}
				det[imp.name] = &detection{
					fp:         w.byName[imp.name],
					version:    imp.version,
					confidence: conf,
				}
				changed = true
			}
		}
	}
}

// applyRequires drops detections whose `requires` / `requiresCategory`
// preconditions are not met, iterating to a fixed point so cascades resolve.
func (w *Wappalyzer) applyRequires(det map[string]*detection) {
	for {
		removed := false
		for name, d := range det {
			if d.fp == nil {
				continue
			}
			if len(d.fp.requires) == 0 && len(d.fp.requiresCat) == 0 {
				continue
			}

			ok := true
			for _, req := range d.fp.requires {
				if _, present := det[req]; !present {
					ok = false
					break
				}
			}
			if ok && len(d.fp.requiresCat) > 0 {
				catOK := false
				for _, c := range d.fp.requiresCat {
					if categoryPresent(det, c) {
						catOK = true
						break
					}
				}
				ok = catOK
			}

			if !ok {
				delete(det, name)
				removed = true
			}
		}
		if !removed {
			return
		}
	}
}

func (w *Wappalyzer) applyExcludes(det map[string]*detection) {
	for _, d := range det {
		if d.fp == nil {
			continue
		}
		for _, ex := range d.fp.excludes {
			delete(det, ex)
		}
	}
}

func (w *Wappalyzer) buildMatches(det map[string]*detection) []Match {
	matches := make([]Match, 0, len(det))
	for name, d := range det {
		if d.confidence <= 0 {
			continue
		}
		m := Match{Name: name, Version: d.version, Confidence: d.confidence}
		if d.fp != nil {
			for _, c := range d.fp.cats {
				if cn, ok := w.categories[c]; ok {
					m.Categories = append(m.Categories, cn)
				}
			}
		}
		matches = append(matches, m)
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Confidence != matches[j].Confidence {
			return matches[i].Confidence > matches[j].Confidence
		}
		return matches[i].Name < matches[j].Name
	})
	return matches
}

func categoryPresent(det map[string]*detection, cat int) bool {
	for _, d := range det {
		if d.fp == nil {
			continue
		}
		for _, c := range d.fp.cats {
			if c == cat {
				return true
			}
		}
	}
	return false
}

func snapshot(det map[string]*detection) []*detection {
	out := make([]*detection, 0, len(det))
	for _, d := range det {
		out = append(out, d)
	}
	return out
}

func pickVersion(cur, next string) string {
	if next == "" {
		return cur
	}
	if len(next) > len(cur) {
		return next
	}
	return cur
}

// normInput is Input with header/cookie/meta/attribute names lower-cased for
// case-insensitive matching.
type normInput struct {
	url       string
	headers   map[string][]string
	cookies   map[string]string
	meta      map[string][]string
	html      string
	scriptSrc []string
	js        map[string]string
	dom       map[string]DOMNode
}

func normalize(in *Input) *normInput {
	n := &normInput{
		url:       in.URL,
		html:      in.HTML,
		scriptSrc: in.ScriptSrc,
		js:        in.JS,
	}

	if len(in.Headers) > 0 {
		n.headers = make(map[string][]string, len(in.Headers))
		for k, v := range in.Headers {
			n.headers[strings.ToLower(k)] = v
		}
	}
	if len(in.Cookies) > 0 {
		n.cookies = make(map[string]string, len(in.Cookies))
		for k, v := range in.Cookies {
			n.cookies[strings.ToLower(k)] = v
		}
	}
	if len(in.Meta) > 0 {
		n.meta = make(map[string][]string, len(in.Meta))
		for k, v := range in.Meta {
			n.meta[strings.ToLower(k)] = v
		}
	}
	if len(in.DOM) > 0 {
		n.dom = make(map[string]DOMNode, len(in.DOM))
		for sel, node := range in.DOM {
			if len(node.Attributes) > 0 {
				lower := make(map[string]string, len(node.Attributes))
				for a, v := range node.Attributes {
					lower[strings.ToLower(a)] = v
				}
				node.Attributes = lower
			}
			n.dom[sel] = node
		}
	}
	return n
}

// matchAll evaluates every field of a fingerprint against the input, returning
// whether it matched at all, the resolved version, and the summed confidence
// (capped at 100).
func (fp *fingerprint) matchAll(in *normInput) (bool, string, int) {
	matched := false
	version := ""
	conf := 0

	record := func(ok bool, v string, c int) {
		if !ok {
			return
		}
		matched = true
		conf += c
		version = pickVersion(version, v)
	}

	for name, pats := range fp.headers {
		for _, val := range in.headers[name] {
			for _, p := range pats {
				ok, v := p.match(val)
				record(ok, v, p.confidence)
			}
		}
	}

	for name, pats := range fp.cookies {
		val, present := in.cookies[name]
		if !present {
			continue
		}
		for _, p := range pats {
			ok, v := p.match(val)
			record(ok, v, p.confidence)
		}
	}

	for name, pats := range fp.meta {
		for _, val := range in.meta[name] {
			for _, p := range pats {
				ok, v := p.match(val)
				record(ok, v, p.confidence)
			}
		}
	}

	for path, pats := range fp.js {
		val, present := in.js[path]
		if !present {
			continue
		}
		for _, p := range pats {
			ok, v := p.match(val)
			record(ok, v, p.confidence)
		}
	}

	for _, p := range fp.html {
		ok, v := p.match(in.html)
		record(ok, v, p.confidence)
	}

	for _, p := range fp.url {
		ok, v := p.match(in.url)
		record(ok, v, p.confidence)
	}

	for _, p := range fp.scriptSrc {
		for _, src := range in.scriptSrc {
			ok, v := p.match(src)
			record(ok, v, p.confidence)
		}
	}

	for _, sel := range fp.domExists {
		if node, ok := in.dom[sel]; ok && node.Exists {
			record(true, "", 100)
		}
	}

	for sel, ds := range fp.domSpecs {
		node, ok := in.dom[sel]
		if !ok {
			continue
		}
		if ds.exists {
			record(true, "", 100)
		}
		for _, p := range ds.text {
			ok, v := p.match(node.Text)
			record(ok, v, p.confidence)
		}
		for attr, pats := range ds.attributes {
			if val, ok := node.Attributes[attr]; ok {
				for _, p := range pats {
					ok, v := p.match(val)
					record(ok, v, p.confidence)
				}
			}
		}
		for prop, pats := range ds.properties {
			if val, ok := node.Properties[prop]; ok {
				for _, p := range pats {
					ok, v := p.match(val)
					record(ok, v, p.confidence)
				}
			}
		}
	}

	if !matched {
		return false, "", 0
	}
	if conf > 100 {
		conf = 100
	}
	return true, version, conf
}
