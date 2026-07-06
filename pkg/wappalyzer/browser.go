package wappalyzer

import (
	"encoding/json"
	"sort"
	"strings"
)

// BrowserResult is the payload returned by the in-browser collector script
// (CollectorJS). Its shape matches the JSON the script emits.
type BrowserResult struct {
	ScriptSrc []string            `json:"scriptSrc"`
	Meta      map[string][]string `json:"meta"`
	JS        map[string]string   `json:"js"`
	DOM       map[string]DOMNode  `json:"dom"`
}

// ApplyTo folds a BrowserResult into an Input, leaving any header/cookie/HTML/URL
// signals the caller already populated intact.
func (r *BrowserResult) ApplyTo(in *Input) {
	if r == nil {
		return
	}
	in.ScriptSrc = append(in.ScriptSrc, r.ScriptSrc...)
	in.JS = r.JS
	in.DOM = r.DOM

	if len(r.Meta) == 0 {
		return
	}
	if in.Meta == nil {
		in.Meta = make(map[string][]string, len(r.Meta))
	}
	for k, v := range r.Meta {
		in.Meta[k] = append(in.Meta[k], v...)
	}
}

// CollectorJS returns a self-contained JavaScript function expression that,
// when invoked in the page context of a loaded document, returns a JSON object
// matching BrowserResult. It embeds the js-property paths and DOM selectors the
// fingerprint database cares about, so the browser only reports back the handful
// of values the engine can actually use.
//
// The value is a function (not self-invoked) so go-rod's page.Eval detects the
// function and calls it. chromedp callers should use CollectorExpr instead.
func (w *Wappalyzer) CollectorJS() string { return w.collectorJS }

// CollectorExpr returns the collector as a self-invoked expression suitable for
// chromedp.Evaluate, which does not call function values itself. It is
// precomputed once so hot-path callers avoid rebuilding the (large) string.
func (w *Wappalyzer) CollectorExpr() string { return w.collectorExpr }

type domCollect struct {
	S string   `json:"s"`
	A []string `json:"a,omitempty"`
	P []string `json:"p,omitempty"`
	T bool     `json:"t,omitempty"`
}

func (w *Wappalyzer) buildCollectorJS() string {
	jsSet := make(map[string]struct{})
	domSet := make(map[string]*domCollect)

	ensureDOM := func(sel string) *domCollect {
		dc, ok := domSet[sel]
		if !ok {
			dc = &domCollect{S: sel}
			domSet[sel] = dc
		}
		return dc
	}

	for _, fp := range w.fingerprints {
		for path := range fp.js {
			jsSet[path] = struct{}{}
		}
		for _, sel := range fp.domExists {
			ensureDOM(sel)
		}
		for sel, ds := range fp.domSpecs {
			dc := ensureDOM(sel)
			if len(ds.text) > 0 {
				dc.T = true
			}
			for a := range ds.attributes {
				dc.A = append(dc.A, a)
			}
			for p := range ds.properties {
				dc.P = append(dc.P, p)
			}
		}
	}

	jsPaths := make([]string, 0, len(jsSet))
	for p := range jsSet {
		jsPaths = append(jsPaths, p)
	}
	sort.Strings(jsPaths)

	domSpecs := make([]*domCollect, 0, len(domSet))
	for _, dc := range domSet {
		sort.Strings(dc.A)
		sort.Strings(dc.P)
		domSpecs = append(domSpecs, dc)
	}
	sort.Slice(domSpecs, func(i, j int) bool { return domSpecs[i].S < domSpecs[j].S })

	jsJSON, _ := json.Marshal(jsPaths)
	domJSON, _ := json.Marshal(domSpecs)

	script := collectorTemplate
	script = strings.Replace(script, "__JS_PATHS__", string(jsJSON), 1)
	script = strings.Replace(script, "__DOM_SPECS__", string(domJSON), 1)
	return script
}

// collectorTemplate is evaluated in the page's main JS world. It is defensive:
// every property/selector access is wrapped so a hostile or broken page cannot
// abort the whole collection.
const collectorTemplate = `function () {
  var out = { scriptSrc: [], meta: {}, js: {}, dom: {} };

  try {
    var scripts = document.scripts || [];
    for (var i = 0; i < scripts.length; i++) {
      if (scripts[i].src) out.scriptSrc.push(scripts[i].src);
    }
  } catch (e) {}

  try {
    var metas = document.querySelectorAll('meta');
    for (var i = 0; i < metas.length; i++) {
      var m = metas[i];
      var n = (m.getAttribute('name') || m.getAttribute('property') || m.getAttribute('http-equiv') || '').toLowerCase();
      if (!n) continue;
      if (!out.meta[n]) out.meta[n] = [];
      out.meta[n].push(m.getAttribute('content') || '');
    }
  } catch (e) {}

  var jsPaths = __JS_PATHS__;
  for (var i = 0; i < jsPaths.length; i++) {
    try {
      var parts = jsPaths[i].split('.');
      var o = window;
      var ok = true;
      for (var j = 0; j < parts.length; j++) {
        if (o == null) { ok = false; break; }
        o = o[parts[j]];
      }
      if (!ok || o === undefined || o === null) continue;
      var t = typeof o;
      out.js[jsPaths[i]] = (t === 'object' || t === 'function') ? '' : String(o);
    } catch (e) {}
  }

  var domSpecs = __DOM_SPECS__;
  for (var k = 0; k < domSpecs.length; k++) {
    var spec = domSpecs[k];
    var el = null;
    try { el = document.querySelector(spec.s); } catch (e) { el = null; }
    if (!el) continue;
    var node = { exists: true };
    try {
      if (spec.t) node.text = (el.textContent || '').slice(0, 3000);
      if (spec.a) {
        node.attributes = {};
        for (var a = 0; a < spec.a.length; a++) {
          var av = el.getAttribute(spec.a[a]);
          if (av != null) node.attributes[spec.a[a]] = av;
        }
      }
      if (spec.p) {
        node.properties = {};
        for (var p = 0; p < spec.p.length; p++) {
          try {
            var pv = el[spec.p[p]];
            if (pv != null) node.properties[spec.p[p]] = String(pv);
          } catch (e) {}
        }
      }
    } catch (e) {}
    out.dom[spec.s] = node;
  }

  return out;
}`
