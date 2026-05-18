package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sensepost/gowitness/pkg/log"
	"github.com/sensepost/gowitness/pkg/models"
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

	// Upsert: create or update
	result := h.DB.Where("result_id = ?", resultID).First(&models.Review{})
	if result.RowsAffected == 0 {
		if err := h.DB.Create(&review).Error; err != nil {
			log.Error("could not create review", "err", err)
			http.Error(w, `{"error":"db error"}`, http.StatusInternalServerError)
			return
		}
	} else {
		if err := h.DB.Model(&models.Review{}).Where("result_id = ?", resultID).Updates(map[string]interface{}{
			"status":     req.Status,
			"comment":    req.Comment,
			"updated_at": time.Now(),
		}).Error; err != nil {
			log.Error("could not update review", "err", err)
			http.Error(w, `{"error":"db error"}`, http.StatusInternalServerError)
			return
		}
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
		result := h.DB.Where("result_id = ?", id).First(&models.Review{})
		if result.RowsAffected == 0 {
			h.DB.Create(&review)
		} else {
			h.DB.Model(&models.Review{}).Where("result_id = ?", id).Updates(map[string]interface{}{
				"status":     req.Status,
				"updated_at": now,
			})
		}
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
			counts["unseen"] += sc.Count
		} else {
			counts[sc.Status] = sc.Count
			tagged += sc.Count
		}
	}
	counts["unseen"] = total - tagged

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
	reviewFilter := r.URL.Query().Get("review")

	var urls []string
	query := h.DB.Model(&models.Result{}).Select("url").Where("failed = ?", false)

	if reviewFilter != "" {
		switch reviewFilter {
		case "unseen":
			query.Where("id NOT IN (?)", h.DB.Model(&models.Review{}).
				Select("result_id").Where("status != ''"))
		case "commented":
			query.Where("id IN (?)", h.DB.Model(&models.Review{}).
				Select("result_id").Where("comment != ''"))
		default:
			query.Where("id IN (?)", h.DB.Model(&models.Review{}).
				Select("result_id").Where("status = ?", reviewFilter))
		}
	}

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
		Where("reviews.comment != '' OR reviews.status IN ('attention', 'vuln', 'interesting')").
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
