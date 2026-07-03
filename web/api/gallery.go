package api

import (
	"encoding/json"
	"net/http"
	"strconv"
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
	// ClusterSize is the number of results in this result's visual (perception-
	// hash) cluster. Only populated in collapse mode; 0 otherwise.
	ClusterSize int64 `json:"cluster_size"`
}

// toGalleryContent maps a Result to the gallery response shape. clusterSize is
// the visual-cluster member count (collapse mode) or 0.
func toGalleryContent(result *models.Result, clusterSize int64) *galleryContent {
	var technologies []string
	for _, tech := range result.Technologies {
		technologies = append(technologies, tech.Value)
	}

	var reviewStatus, reviewComment string
	if result.Review != nil {
		reviewStatus = result.Review.Status
		reviewComment = result.Review.Comment
	}

	return &galleryContent{
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
		ClusterSize:   clusterSize,
	}
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
//	@Param			review			query		string	false	"Filter by review status (done,attention,interesting,vuln,junk,fuzz,unseen,commented)."
//	@Success		200				{object}	galleryResponse
//	@Router			/results/gallery [get]
func (h *ApiHandler) GalleryHandler(w http.ResponseWriter, r *http.Request) {
	var results = &galleryResponse{
		Page:    1,
		Limit:   96,
		Results: []*galleryContent{},
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

	// shared filters (trash exclusion, status hundreds-class, technology,
	// failed, review) — parsed once and reused for the count query below.
	filters := parseGalleryFilters(r)

	// sort order
	sortOrder := r.URL.Query().Get("sort")

	// collapse mode: one representative per visual (perception-hash) cluster
	if collapse, _ := strconv.ParseBool(r.URL.Query().Get("collapse")); collapse {
		h.galleryCollapsed(w, results, filters, sortOrder, offset)
		return
	}

	// query the db
	var queryResults []*models.Result
	query := h.DB.Model(&models.Result{}).Limit(results.Limit).
		Offset(offset).Preload("Technologies").Preload("Review")

	if perceptionSort {
		query = query.Order("perception_hash_group_id DESC")
	}

	switch sortOrder {
	case "newest":
		query = query.Order("probed_at DESC").Order("id DESC")
	case "oldest":
		query = query.Order("probed_at ASC").Order("id ASC")
	}

	query = filters.apply(query, h)

	// run the query
	if err := query.Find(&queryResults).Error; err != nil {
		log.Error("could not get gallery", "err", err)
		return
	}

	for _, result := range queryResults {
		results.Results = append(results.Results, toGalleryContent(result, 0))
	}

	// Build a count query with the same filters (without limit/offset)
	countQuery := filters.apply(h.DB.Model(&models.Result{}), h)
	if err := countQuery.Count(&results.TotalCount).Error; err != nil {
		log.Error("could not count total results", "err", err)
		return
	}

	writeJSON(w, results)
}

// clusterGroupExpr collapses results by (root_domain + gowitness's perception
// GROUP). Two deliberate choices:
//   - The perception GROUP, not the exact hash. CDN/WAF pages like CloudFront
//     "ERROR: The request could not be satisfied" or Cloudflare "Checking your
//     browser" render with tiny per-request pixel differences, so the exact hash
//     differs on nearly every host — even :80 vs :443 of the SAME host. Grouping
//     by exact hash therefore does NOT collapse them and produces duplicate
//     cards for the same page/host. gowitness's perception group (small Hamming
//     distance) merges these near-identical variants into one card.
//   - Scoped to root_domain. The fuzzy group alone chains visually-similar but
//     unrelated sites ACROSS domains (e.g. baiclaw.b.ai + avatar.winnfthero.io),
//     which would hide a distinct target. Scoping to the registrable domain
//     keeps every domain its own card, while different-looking pages still land
//     in different groups (Hamming distance > threshold) — so real targets are
//     not hidden either.
//
// Ungrouped rows (group id 0 — e.g. failed screenshots) are never collapsed: the
// CASE gives each its own id so it stays a distinct card.
const clusterGroupExpr = "root_domain, perception_hash_group_id, CASE WHEN perception_hash_group_id > 0 THEN 0 ELSE id END"

// galleryCollapsed returns one representative result per visual cluster, with
// the cluster's member count, so large recon lists (where a wildcard yields
// thousands of identical default pages) surface only the distinct screens.
func (h *ApiHandler) galleryCollapsed(w http.ResponseWriter, results *galleryResponse, filters galleryFilters, sortOrder string, offset int) {
	// pick a representative (lowest id) + member count per cluster
	type clusterRep struct {
		ID          uint
		ClusterSize int64
	}
	repQuery := filters.apply(h.DB.Model(&models.Result{}), h).
		Select("MIN(id) as id, COUNT(*) as cluster_size").
		Group(clusterGroupExpr)

	switch sortOrder {
	case "newest":
		// MIN(id) is unique per cluster — a stable tiebreaker so paginated
		// clusters that share a timestamp can't shift/duplicate across pages.
		repQuery = repQuery.Order("MAX(probed_at) DESC").Order("MIN(id) DESC")
	case "oldest":
		repQuery = repQuery.Order("MIN(probed_at) ASC").Order("MIN(id) ASC")
	default:
		repQuery = repQuery.Order("MIN(id) ASC")
	}

	var reps []clusterRep
	if err := repQuery.Limit(results.Limit).Offset(offset).Scan(&reps).Error; err != nil {
		log.Error("could not get collapsed gallery", "err", err)
		return
	}

	// fetch the full representative rows and re-attach the cluster sizes
	repIDs := make([]uint, 0, len(reps))
	sizeByID := make(map[uint]int64, len(reps))
	for _, rp := range reps {
		repIDs = append(repIDs, rp.ID)
		sizeByID[rp.ID] = rp.ClusterSize
	}

	if len(repIDs) > 0 {
		var full []*models.Result
		if err := h.DB.Preload("Technologies").Preload("Review").
			Where("id IN ?", repIDs).Find(&full).Error; err != nil {
			log.Error("could not load collapsed representatives", "err", err)
			return
		}
		byID := make(map[uint]*models.Result, len(full))
		for _, res := range full {
			byID[res.ID] = res
		}
		// preserve the representative ordering from repQuery
		for _, id := range repIDs {
			if res := byID[id]; res != nil {
				results.Results = append(results.Results, toGalleryContent(res, sizeByID[id]))
			}
		}
	}

	// total = number of clusters matching the filters
	sub := filters.apply(h.DB.Model(&models.Result{}), h).Select("1").Group(clusterGroupExpr)
	if err := h.DB.Table("(?) as clusters", sub).Count(&results.TotalCount).Error; err != nil {
		log.Error("could not count clusters", "err", err)
		return
	}

	writeJSON(w, results)
}

// writeJSON marshals v and writes it, logging on failure.
func writeJSON(w http.ResponseWriter, v any) {
	jsonData, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(jsonData)
}
