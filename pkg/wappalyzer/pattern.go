package wappalyzer

import (
	"regexp"
	"strconv"
	"strings"
)

// pattern is a single compiled Wappalyzer fingerprint pattern. A Wappalyzer
// pattern string looks like `regex\;version:\1\;confidence:50`, where the
// leading segment is a (case-insensitive) regular expression and the trailing
// `\;key:value` segments are modifiers.
type pattern struct {
	regex      *regexp.Regexp // nil means "presence only" (empty pattern)
	version    string         // version template, may contain \N backrefs / ternary
	confidence int            // 0-100, defaults to 100
}

// parsePattern parses one raw Wappalyzer pattern string. It returns nil if the
// regex portion cannot be compiled by RE2 (Go does not support the JS lookahead
// / backreference constructs some fingerprints use); such patterns are skipped,
// mirroring the behaviour of the reference implementations.
func parsePattern(raw string) *pattern {
	parts := strings.Split(raw, `\;`)
	p := &pattern{confidence: 100}

	for _, mod := range parts[1:] {
		kv := strings.SplitN(mod, ":", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "version":
			p.version = kv[1]
		case "confidence":
			if n, err := strconv.Atoi(strings.TrimSpace(kv[1])); err == nil {
				p.confidence = n
			}
		}
	}

	expr := parts[0]
	if expr == "" {
		// Empty regex: the pattern matches purely on the presence of the field.
		return p
	}

	re, err := regexp.Compile("(?i)" + expr)
	if err != nil {
		// Try a light cleanup of the most common RE2-incompatible bits before
		// giving up entirely.
		if cleaned := sanitizeRegex(expr); cleaned != expr {
			if re, err = regexp.Compile("(?i)" + cleaned); err != nil {
				return nil
			}
		} else {
			return nil
		}
	}
	p.regex = re
	return p
}

// sanitizeRegex strips the most common JS regex constructs RE2 rejects so a
// pattern still has a chance of compiling instead of being dropped outright.
func sanitizeRegex(expr string) string {
	// Drop lookahead / lookbehind groups: (?=...) (?!...) (?<=...) (?<!...)
	lookaround := regexp.MustCompile(`\(\?<?[=!][^)]*\)`)
	return lookaround.ReplaceAllString(expr, "")
}

// match tests the pattern against a value. It reports whether the pattern
// matched, and if so the resolved version string (possibly empty).
func (p *pattern) match(value string) (bool, string) {
	if p.regex == nil {
		// Presence-only pattern.
		return true, resolveVersion(p.version, nil)
	}
	m := p.regex.FindStringSubmatch(value)
	if m == nil {
		return false, ""
	}
	return true, resolveVersion(p.version, m)
}

// resolveVersion applies the Wappalyzer version template rules against the
// regex submatches: `\N` backreferences and the `\N?present:absent` ternary
// form. It is a faithful port of Wappalyzer's resolveVersion.
func resolveVersion(version string, matches []string) string {
	if version == "" {
		return ""
	}
	resolved := version

	for i, m := range matches {
		if i == 0 {
			// Group 0 is the full match; Wappalyzer still allows \0 refs.
		}
		idx := strconv.Itoa(i)

		// Ternary: \N?present:absent
		ternary := regexp.MustCompile(`\\` + idx + `\?([^:]+):(.*)$`)
		if t := ternary.FindStringSubmatch(resolved); len(t) == 3 {
			replacement := t[2]
			if m != "" {
				replacement = t[1]
			}
			resolved = strings.Replace(resolved, t[0], replacement, 1)
		}

		// Plain backreference: \N -> match (or empty).
		resolved = strings.ReplaceAll(resolved, `\`+idx, m)
	}

	return strings.TrimSpace(resolved)
}
