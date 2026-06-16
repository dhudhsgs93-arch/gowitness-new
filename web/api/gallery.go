package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sensepost/gowitness/pkg/log"
	"github.com/sensepost/gowitness/pkg/models"
)

type galleryResponse struct {
	Results    []*galleryContent `json:"results"`
	Page       int               `json:"page"`
	Limit      int               `json:"limit"`
	TotalCount int64             `json:"total_count"`
}

type galleryContent struct {
	ID            uint      `json:"id"`
	ProbedAt      time.Time `json:"probed_at"`
	URL           string    `json:"url"`
	ResponseCode  int       `json:"response_code"`
	Title         string    `json:"title"`
	Filename      string    `json:"file_name"`
	Screenshot    string    `json:"screenshot"`
	Failed        bool      `json:"failed"`
	Technologies  []string  `json:"technologies"`
	ReviewStatus  string    `json:"review_status"`
	ReviewComment string    `json:"review_comment"`
}

// GalleryHandler gets a paginated gallery
//
//	@Summary		Gallery
//	@Description	Get a paginated list of results.
//	@Tags			Results
//	@Accept			json
//	@Produce		json
//	@Param			page			query		int		false	"The page to load."
//	@Param			limit			query		int		false	"Number of results per page."
//	@Param			technologies	query		string	false	"A comma seperated list of technologies to filter by."
//	@Param			status			query		string	false	"A comma seperated list of HTTP status codes to filter by."
//	@Param			perception		query		boolean	false	"Order the results by perception hash."
//	@Param			failed			query		boolean	false	"Include failed screenshots in the results."
//	@Param			review			query		string	false	"Filter by review status (done,attention,interesting,vuln,junk,unseen,commented)."
//	@Success		200				{object}	galleryResponse
//	@Router			/results/gallery [get]
func (h *ApiHandler) GalleryHandler(w http.ResponseWriter, r *http.Request) {
	var results = &galleryResponse{
		Page:  1,
		Limit: 96,
	}

	// pagination
	urlPage := r.URL.Query().Get("page")
	urlLimit := r.URL.Query().Get("limit")
	if p, err := strconv.Atoi(urlPage); err == nil && p > 0 {
		results.Page = p
	}
	if l, err := strconv.Atoi(urlLimit); err == nil && l > 0 {
		results.Limit = l
	}
	offset := (results.Page - 1) * results.Limit

	// perception sorting
	var perceptionSort bool
	perceptionSortValue := r.URL.Query().Get("perception")
	perceptionSort, err := strconv.ParseBool(perceptionSortValue)
	if err != nil {
		perceptionSort = false
	}

	// status code filtering.
	// Each selected code matches its whole hundreds-class, so pressing "500"
	// also matches 503, 522, etc. (and "200" -> 2xx, "403" -> 4xx).
	var statusCodes []int
	statusFilterValue := r.URL.Query().Get("status")
	if statusFilterValue != "" {
		for _, statusCodeString := range strings.Split(statusFilterValue, ",") {
			statusCode, err := strconv.Atoi(statusCodeString)
			if err != nil {
				continue
			}

			statusCodes = append(statusCodes, statusCode)
		}
	}
	// build an OR-of-ranges clause from the unique hundreds-buckets
	var statusClause string
	var statusArgs []interface{}
	if len(statusCodes) > 0 {
		buckets := make(map[int]bool)
		for _, c := range statusCodes {
			buckets[(c/100)*100] = true
		}
		parts := make([]string, 0, len(buckets))
		for b := range buckets {
			parts = append(parts, "(response_code >= ? AND response_code < ?)")
			statusArgs = append(statusArgs, b, b+100)
		}
		statusClause = strings.Join(parts, " OR ")
	}

	// technology filtering
	var technologies []string
	technologyFilterValue := r.URL.Query().Get("technologies")
	if technologyFilterValue != "" {
		technologies = append(technologies, strings.Split(technologyFilterValue, ",")...)
	}

	// failed result filtering
	var showFailed bool
	showFailed, err = strconv.ParseBool(r.URL.Query().Get("failed"))
	if err != nil {
		showFailed = true
	}

	// review status filtering
	reviewFilter := r.URL.Query().Get("review")

	// sort order
	sortOrder := r.URL.Query().Get("sort")

	// query the db
	var queryResults []*models.Result
	query := h.DB.Model(&models.Result{}).Limit(results.Limit).
		Offset(offset).Preload("Technologies").Preload("Review")

	// Exclude trashed domains (substring match)
	query.Where("NOT EXISTS (SELECT 1 FROM trashed_hosts th WHERE results.hostname LIKE '%' || th.host || '%')")

	if perceptionSort {
		query.Order("perception_hash_group_id DESC")
	}

	switch sortOrder {
	case "newest":
		query.Order("probed_at DESC")
	case "oldest":
		query.Order("probed_at ASC")
	}

	if statusClause != "" {
		query.Where(statusClause, statusArgs...)
	}

	if len(technologies) > 0 {
		query.Where("id in (?)", h.DB.Model(&models.Technology{}).
			Select("result_id").Distinct("result_id").
			Where("value IN (?)", technologies))
	}

	if !showFailed {
		query.Where("failed = ?", showFailed)
	}

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

	// run the query
	if err := query.Find(&queryResults).Error; err != nil {
		log.Error("could not get gallery", "err", err)
		return
	}

	// extract Technologies for each result
	for _, result := range queryResults {
		var technologies []string
		for _, tech := range result.Technologies {
			technologies = append(technologies, tech.Value)
		}

		var reviewStatus, reviewComment string
		if result.Review != nil {
			reviewStatus = result.Review.Status
			reviewComment = result.Review.Comment
		}

		// Append the processed data to the response
		results.Results = append(results.Results, &galleryContent{
			ID:            result.ID,
			ProbedAt:      result.ProbedAt,
			URL:           result.URL,
			ResponseCode:  result.ResponseCode,
			Title:         result.Title,
			Filename:      result.Filename,
			Screenshot:    result.Screenshot,
			Failed:        result.Failed,
			Technologies:  technologies,
			ReviewStatus:  reviewStatus,
			ReviewComment: reviewComment,
		})
	}

	// Build a count query with the same filters (without limit/offset)
	countQuery := h.DB.Model(&models.Result{})
	// Exclude trashed hosts from count (substring match)
	countQuery.Where("NOT EXISTS (SELECT 1 FROM trashed_hosts th WHERE results.hostname LIKE '%' || th.host || '%')")
	if statusClause != "" {
		countQuery.Where(statusClause, statusArgs...)
	}
	if len(technologies) > 0 {
		countQuery.Where("id in (?)", h.DB.Model(&models.Technology{}).
			Select("result_id").Distinct("result_id").
			Where("value IN (?)", technologies))
	}
	if !showFailed {
		countQuery.Where("failed = ?", showFailed)
	}
	if reviewFilter != "" {
		switch reviewFilter {
		case "unseen":
			countQuery.Where("id NOT IN (?)", h.DB.Model(&models.Review{}).
				Select("result_id").Where("status != ''"))
		case "commented":
			countQuery.Where("id IN (?)", h.DB.Model(&models.Review{}).
				Select("result_id").Where("comment != ''"))
		default:
			countQuery.Where("id IN (?)", h.DB.Model(&models.Review{}).
				Select("result_id").Where("status = ?", reviewFilter))
		}
	}
	if err := countQuery.Count(&results.TotalCount).Error; err != nil {
		log.Error("could not count total results", "err", err)
		return
	}

	jsonData, err := json.Marshal(results)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(jsonData)
}
