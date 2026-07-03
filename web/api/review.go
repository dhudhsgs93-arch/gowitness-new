package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sensepost/gowitness/pkg/log"
	"github.com/sensepost/gowitness/pkg/models"
	"gorm.io/gorm/clause"
)

type reviewRequest struct {
	Status  string `json:"status"`
	Comment string `json:"comment"`
}

type bulkReviewRequest struct {
	IDs    []uint `json:"ids"`
	Status string `json:"status"`
}

type reviewStatsResponse struct {
	Total     int64            `json:"total"`
	Counts    map[string]int64 `json:"counts"`
	Commented int64            `json:"commented"`
}

// ReviewSetHandler sets or updates a review for a result
//
//	@Summary		Set review
//	@Description	Set or update a review (status + comment) for a result.
//	@Tags			Reviews
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int				true	"The result ID."
//	@Param			body	body		reviewRequest	true	"Review data"
//	@Success		200		{object}	map[string]bool
//	@Router			/review/{id} [post]
func (h *ApiHandler) ReviewSetHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	resultID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}

	var req reviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	review := models.Review{
		ResultID:  uint(resultID),
		Status:    req.Status,
		Comment:   req.Comment,
		UpdatedAt: time.Now(),
	}

	// Atomic upsert keyed on the unique result_id column. The old
	// read-then-write pattern raced: two concurrent requests for the same id
	// could both see "not found" and both INSERT, tripping the unique index.
	if err := h.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "result_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "comment", "updated_at"}),
	}).Create(&review).Error; err != nil {
		log.Error("could not upsert review", "err", err)
		http.Error(w, `{"error":"db error"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// ReviewGetHandler gets the review for a result
//
//	@Summary		Get review
//	@Description	Get the review for a result.
//	@Tags			Reviews
//	@Produce		json
//	@Param			id	path		int	true	"The result ID."
//	@Success		200	{object}	models.Review
//	@Router			/review/{id} [get]
func (h *ApiHandler) ReviewGetHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	resultID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}

	var review models.Review
	if err := h.DB.Where("result_id = ?", resultID).First(&review).Error; err != nil {
		// Return empty review if not found
		json.NewEncoder(w).Encode(models.Review{ResultID: uint(resultID)})
		return
	}

	json.NewEncoder(w).Encode(review)
}

// ReviewBulkHandler sets status for multiple results at once
//
//	@Summary		Bulk review
//	@Description	Set status for multiple results at once.
//	@Tags			Reviews
//	@Accept			json
//	@Produce		json
//	@Param			body	body		bulkReviewRequest	true	"Bulk review data"
//	@Success		200		{object}	map[string]interface{}
//	@Router			/review/bulk [post]
func (h *ApiHandler) ReviewBulkHandler(w http.ResponseWriter, r *http.Request) {
	var req bulkReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	now := time.Now()
	for _, id := range req.IDs {
		review := models.Review{
			ResultID:  id,
			Status:    req.Status,
			UpdatedAt: now,
		}
		// Upsert the status only, leaving any existing comment intact.
		h.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "result_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"status", "updated_at"}),
		}).Create(&review)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "count": len(req.IDs)})
}

// ReviewStatsHandler returns review statistics
//
//	@Summary		Review stats
//	@Description	Get review statistics (counts by status).
//	@Tags			Reviews
//	@Produce		json
//	@Success		200	{object}	reviewStatsResponse
//	@Router			/review/stats [get]
func (h *ApiHandler) ReviewStatsHandler(w http.ResponseWriter, r *http.Request) {
	var total int64
	h.DB.Model(&models.Result{}).Where("failed = ?", false).Count(&total)

	// Count by status
	type statusCount struct {
		Status string
		Count  int64
	}
	var statusCounts []statusCount
	h.DB.Model(&models.Review{}).
		Select("status, count(*) as count").
		Group("status").
		Find(&statusCounts)

	counts := make(map[string]int64)
	var tagged int64
	for _, sc := range statusCounts {
		if sc.Status == "" {
			continue // rows with an empty status are not "tagged"
		}
		counts[sc.Status] = sc.Count
		tagged += sc.Count
	}
	// unseen = results with no (non-empty) status. Clamp at 0: a review can
	// outlive its result in edge cases, which would otherwise go negative.
	unseen := total - tagged
	if unseen < 0 {
		unseen = 0
	}
	counts["unseen"] = unseen

	var commented int64
	h.DB.Model(&models.Review{}).Where("comment != ''").Count(&commented)

	resp := reviewStatsResponse{
		Total:     total,
		Counts:    counts,
		Commented: commented,
	}

	json.NewEncoder(w).Encode(resp)
}

// ReviewExportURLsHandler exports URLs filtered by review status as plain text
//
//	@Summary		Export URLs
//	@Description	Export URLs filtered by review status as plain text (one per line).
//	@Tags			Reviews
//	@Produce		text/plain
//	@Param			review	query	string	false	"Filter by review status."
//	@Router			/review/export-urls [get]
func (h *ApiHandler) ReviewExportURLsHandler(w http.ResponseWriter, r *http.Request) {
	// Mirror the gallery's filters (status, technology, failed, review) and
	// exclude trashed hosts, so the exported URLs match exactly what the user
	// currently sees rather than silently including hidden/filtered results.
	filters := parseGalleryFilters(r)

	var urls []string
	query := filters.apply(h.DB.Model(&models.Result{}).Select("url"), h)
	query.Find(&urls)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=urls.txt")
	for _, u := range urls {
		w.Write([]byte(u + "\n"))
	}
}

// ReviewExportHandler exports all reviews as markdown
//
//	@Summary		Export reviews
//	@Description	Export all reviewed/commented results as markdown.
//	@Tags			Reviews
//	@Produce		text/markdown
//	@Router			/review/export [get]
func (h *ApiHandler) ReviewExportHandler(w http.ResponseWriter, r *http.Request) {
	type exportRow struct {
		URL          string
		ResponseCode int
		Title        string
		Status       string
		Comment      string
	}

	var rows []exportRow
	h.DB.Model(&models.Result{}).
		Select("results.url, results.response_code, results.title, reviews.status, reviews.comment").
		Joins("JOIN reviews ON reviews.result_id = results.id").
		Where("reviews.status != '' OR reviews.comment != ''").
		Order("reviews.status DESC").
		Find(&rows)

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=gowitness-reviews.md")

	for _, row := range rows {
		line := "## [" + row.Status + "] " + row.URL + " [" + strconv.Itoa(row.ResponseCode) + "]\n"
		if row.Title != "" {
			line += "**" + row.Title + "**\n"
		}
		if row.Comment != "" {
			line += row.Comment + "\n"
		}
		line += "\n"
		w.Write([]byte(line))
	}
}
