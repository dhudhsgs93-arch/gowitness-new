package api

import (
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sensepost/gowitness/pkg/database"
	"github.com/sensepost/gowitness/pkg/wappalyzer"
	"gorm.io/gorm"
)

// ApiHandler is an API handler
type ApiHandler struct {
	DbURI          string
	ScreenshotPath string
	DB             *gorm.DB
	Wappalyzer     *wappalyzer.Wappalyzer

	// icon cache: technology icons are served from this server (same-origin)
	// instead of hot-linking GitHub, so a page loading many icons is reliable
	// and works offline after the first fetch.
	iconMu   sync.Mutex
	iconMem  map[string][]byte
	iconDir  string
	iconHTTP *http.Client
}

// NewApiHandler returns a new ApiHandler
func NewApiHandler(uri string, screenshotPath string) (*ApiHandler, error) {

	// get a db handle
	conn, err := database.Connection(uri, false, false)
	if err != nil {
		return nil, err
	}

	wap, _ := wappalyzer.New()

	// best-effort on-disk icon cache location; falls back to memory-only.
	iconDir := ""
	if cache, err := os.UserCacheDir(); err == nil {
		iconDir = filepath.Join(cache, "gowitness", "wappalyzer-icons")
		if err := os.MkdirAll(iconDir, 0o755); err != nil {
			iconDir = ""
		}
	}

	return &ApiHandler{
		DbURI:          uri,
		ScreenshotPath: screenshotPath,
		DB:             conn,
		Wappalyzer:     wap,
		iconMem:        make(map[string][]byte),
		iconDir:        iconDir,
		iconHTTP:       &http.Client{Timeout: 15 * time.Second},
	}, nil
}
