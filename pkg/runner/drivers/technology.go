package driver

import "github.com/sensepost/gowitness/pkg/models"

// cookieMap flattens the collected cookies into a name->value map for
// wappalyzer cookie fingerprinting.
func cookieMap(cookies []models.Cookie) map[string]string {
	if len(cookies) == 0 {
		return nil
	}
	m := make(map[string]string, len(cookies))
	for _, c := range cookies {
		m[c.Name] = c.Value
	}
	return m
}

// wappURL returns the final URL when known, otherwise the originally requested
// target, for wappalyzer url fingerprinting.
func wappURL(final, target string) string {
	if final != "" {
		return final
	}
	return target
}
