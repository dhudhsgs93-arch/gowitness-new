package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sensepost/gowitness/pkg/log"
	"github.com/sensepost/gowitness/pkg/models"
)

// normalizePattern lowercases and trims the input for substring matching.
func normalizePattern(input string) string {
	return strings.ToLower(strings.TrimSpace(input))
}

type trashAddRequest struct {
	Host string `json:"host"`
}

type trashRestoreRequest struct {
	ID uint `json:"id"`
}

type trashedHostResponse struct {
	ID        uint      `json:"id"`
	Host      string    `json:"host"`
	CreatedAt time.Time `json:"created_at"`
	Count     int64     `json:"count"`
}

// TrashAddHandler adds a host to the trash list
func (h *ApiHandler) TrashAddHandler(w http.ResponseWriter, r *http.Request) {
	var req trashAddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	pattern := normalizePattern(req.Host)
	if pattern == "" {
		http.Error(w, `{"error":"invalid or empty host"}`, http.StatusBadRequest)
		return
	}
	// Guard against catastrophic over-match: trashing a 1-2 char substring
	// (e.g. "co") would hide almost the entire gallery via the substring match.
	if len(pattern) < 3 {
		http.Error(w, `{"error":"host pattern too short (min 3 chars)"}`, http.StatusBadRequest)
		return
	}

	host := pattern

	// Idempotent: find or create
	var existing models.TrashedHost
	result := h.DB.Where("host = ?", host).First(&existing)
	if result.RowsAffected > 0 {
		// Already exists, return it
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "trashed_host": existing})
		return
	}

	entry := models.TrashedHost{
		Host:      host,
		CreatedAt: time.Now(),
	}
	if err := h.DB.Create(&entry).Error; err != nil {
		log.Error("could not trash host", "err", err)
		http.Error(w, `{"error":"db error"}`, http.StatusInternalServerError)
		return
	}

	log.Info("trashed host", "host", host)
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "trashed_host": entry})
}

type trashBulkRequest struct {
	IDs []uint `json:"ids"`
}

// TrashBulkHandler trashes the hostnames of a set of result IDs in one call.
// It resolves each result's canonical hostname (falling back to parsing the
// URL when the hostname column is empty), de-duplicates, and idempotently adds
// each to the trash list. Used by the gallery's bulk-select "Trash" action.
func (h *ApiHandler) TrashBulkHandler(w http.ResponseWriter, r *http.Request) {
	var req trashBulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if len(req.IDs) == 0 {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "hosts": 0, "added": 0})
		return
	}

	var results []models.Result
	h.DB.Select("id", "hostname", "url").Where("id IN (?)", req.IDs).Find(&results)

	// collect unique, non-empty hostnames
	hostSet := make(map[string]struct{})
	for _, res := range results {
		host := normalizePattern(res.Hostname)
		if host == "" {
			// fall back to the URL host (older DBs may have an empty hostname column)
			if u, err := url.Parse(res.URL); err == nil {
				host = normalizePattern(u.Hostname())
			}
		}
		if host != "" {
			hostSet[host] = struct{}{}
		}
	}

	added := 0
	for host := range hostSet {
		var existing models.TrashedHost
		if h.DB.Where("host = ?", host).First(&existing).RowsAffected > 0 {
			continue // already trashed
		}
		if err := h.DB.Create(&models.TrashedHost{Host: host, CreatedAt: time.Now()}).Error; err != nil {
			log.Error("could not bulk-trash host", "host", host, "err", err)
			continue
		}
		added++
	}

	log.Info("bulk trashed hosts", "selected", len(req.IDs), "hosts", len(hostSet), "added", added)
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "hosts": len(hostSet), "added": added})
}

// TrashRestoreHandler removes a host from the trash list by ID
func (h *ApiHandler) TrashRestoreHandler(w http.ResponseWriter, r *http.Request) {
	var req trashRestoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	result := h.DB.Delete(&models.TrashedHost{}, req.ID)
	if result.Error != nil {
		log.Error("could not restore host", "err", result.Error)
		http.Error(w, `{"error":"db error"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// TrashListHandler lists all trashed hosts with result counts
func (h *ApiHandler) TrashListHandler(w http.ResponseWriter, r *http.Request) {
	var hosts []models.TrashedHost
	h.DB.Order("created_at DESC").Find(&hosts)

	// Get counts per trashed host
	var response []trashedHostResponse
	for _, th := range hosts {
		var count int64
		h.DB.Model(&models.Result{}).Where(hostnameContainsExpr(h.DB), th.Host).Count(&count)
		response = append(response, trashedHostResponse{
			ID:        th.ID,
			Host:      th.Host,
			CreatedAt: th.CreatedAt,
			Count:     count,
		})
	}

	if response == nil {
		response = []trashedHostResponse{}
	}

	json.NewEncoder(w).Encode(response)
}

// TrashSuggestHandler returns hostname suggestions for autocomplete
func (h *ApiHandler) TrashSuggestHandler(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	var hosts []string

	query := h.DB.Model(&models.Result{}).
		Select("DISTINCT hostname").
		Where("hostname != ''").
		Where(trashExclusionClause(h.DB)).
		Order("hostname").
		Limit(limit)

	if q != "" {
		query = query.Where("hostname LIKE ?", "%"+q+"%")
	}

	query.Pluck("hostname", &hosts)

	if hosts == nil {
		hosts = []string{}
	}

	json.NewEncoder(w).Encode(hosts)
}
