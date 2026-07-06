package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// iconBase is the upstream location the server lazily fetches technology icons
// from. Icons are cached locally and re-served from this server, so the browser
// never hot-links GitHub directly.
const iconBase = `https://raw.githubusercontent.com/enthec/webappanalyzer/main/src/images/icons/`

// WappalyzerHandler returns wappalyzer data
//
//	@Summary		Get wappalyzer data
//	@Description	Get a technology-name -> icon-URL map. Icon URLs point back at
//	@Description	this server's /wappalyzer/icon endpoint (same-origin, cached).
//	@Tags			Results
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	map[string]string
//	@Router			/wappalyzer [get]
func (h *ApiHandler) WappalyzerHandler(w http.ResponseWriter, r *http.Request) {
	response := make(map[string]string)

	for name, icon := range h.Wappalyzer.Icons() {
		if icon == "" {
			continue
		}

		response[name] = "/wappalyzer/icon?tech=" + url.QueryEscape(name)
	}

	jsonData, err := json.Marshal(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(jsonData)
}

// IconHandler serves a single technology icon by name. It resolves the icon
// filename from the fingerprint database, then serves it from an in-memory /
// on-disk cache, fetching from the upstream once on a miss. Missing icons return
// 404 so the frontend can hide them gracefully.
//
// Mounted OUTSIDE the /api group so the isJSON middleware does not clobber the
// image content-type.
func (h *ApiHandler) IconHandler(w http.ResponseWriter, r *http.Request) {
	tech := r.URL.Query().Get("tech")
	if tech == "" {
		tech = chi.URLParam(r, "tech")
	}

	filename := h.Wappalyzer.IconFile(tech)
	if filename == "" {
		http.NotFound(w, r)
		return
	}

	data, err := h.icon(filename)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", iconContentType(filename))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

// icon returns the bytes for an icon filename, using the memory cache, then the
// on-disk cache, then a one-time upstream fetch.
func (h *ApiHandler) icon(filename string) ([]byte, error) {
	h.iconMu.Lock()
	if b, ok := h.iconMem[filename]; ok {
		h.iconMu.Unlock()
		return b, nil
	}
	h.iconMu.Unlock()

	// on-disk cache
	var diskPath string
	if h.iconDir != "" {
		diskPath = filepath.Join(h.iconDir, filename)
		if b, err := os.ReadFile(diskPath); err == nil {
			h.memoize(filename, b)
			return b, nil
		}
	}

	// upstream fetch
	resp, err := h.iconHTTP.Get(iconBase + iconPathEscape(filename))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	b := make([]byte, 0, 8192)
	buf := make([]byte, 8192)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			b = append(b, buf[:n]...)
		}
		if rerr != nil {
			break
		}
	}

	if diskPath != "" {
		_ = os.WriteFile(diskPath, b, 0o644)
	}
	h.memoize(filename, b)
	return b, nil
}

func (h *ApiHandler) memoize(filename string, b []byte) {
	h.iconMu.Lock()
	h.iconMem[filename] = b
	h.iconMu.Unlock()
}

// iconPathEscape escapes a filename for use as a URL path segment (e.g. spaces
// in "Ant Design.svg").
func iconPathEscape(filename string) string {
	return url.PathEscape(filename)
}

func iconContentType(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".ico":
		return "image/x-icon"
	default:
		return "application/octet-stream"
	}
}
