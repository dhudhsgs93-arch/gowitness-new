package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/sensepost/gowitness/pkg/models"
	"gorm.io/gorm"
)

// galleryFilters is the set of filters shared by the gallery listing, its count
// query, and the URL export, so all three always agree on what is visible.
type galleryFilters struct {
	statusClause string
	statusArgs   []any
	technologies []string
	showFailed   bool
	reviewFilter string
	categoryID   uint
}

// parseGalleryFilters extracts the shared filter set from a request.
func parseGalleryFilters(r *http.Request) galleryFilters {
	f := galleryFilters{showFailed: true}

	// Status filtering. Each selected code matches its whole hundreds-class,
	// so "500" also matches 503/522 (and "200" -> 2xx, "403" -> 4xx). Build an
	// OR-of-ranges from the unique buckets.
	if v := r.URL.Query().Get("status"); v != "" {
		buckets := make(map[int]bool)
		for _, s := range strings.Split(v, ",") {
			if code, err := strconv.Atoi(s); err == nil {
				buckets[(code/100)*100] = true
			}
		}
		parts := make([]string, 0, len(buckets))
		for b := range buckets {
			parts = append(parts, "(response_code >= ? AND response_code < ?)")
			f.statusArgs = append(f.statusArgs, b, b+100)
		}
		f.statusClause = strings.Join(parts, " OR ")
	}

	if v := r.URL.Query().Get("technologies"); v != "" {
		f.technologies = strings.Split(v, ",")
	}

	if showFailed, err := strconv.ParseBool(r.URL.Query().Get("failed")); err == nil {
		f.showFailed = showFailed
	}

	f.reviewFilter = r.URL.Query().Get("review")

	if v := r.URL.Query().Get("category"); v != "" {
		if id, err := strconv.Atoi(v); err == nil && id > 0 {
			f.categoryID = uint(id)
		}
	}

	return f
}

// trashExclusionClause returns a dialect-portable predicate that is true for
// results whose hostname does NOT contain any trashed host as a substring.
// The column-to-column substring test uses instr() on sqlite/mysql and
// strpos() on postgres (the `||` string-concat + LIKE form is not portable:
// on MySQL `||` means logical OR). Matching a needle literally also avoids the
// LIKE wildcard problem where a `%`/`_` in a trashed host widened the match.
func trashExclusionClause(db *gorm.DB) string {
	contains := "instr(results.hostname, th.host) > 0"
	if db.Dialector.Name() == "postgres" {
		contains = "strpos(results.hostname, th.host) > 0"
	}
	return "NOT EXISTS (SELECT 1 FROM trashed_hosts th WHERE th.host <> '' AND " + contains + ")"
}

// hostnameContainsExpr returns a dialect-portable, parameterized predicate that
// is true when results.hostname contains the bound value as a literal
// substring. Same semantics as trashExclusionClause, for single-value queries.
func hostnameContainsExpr(db *gorm.DB) string {
	if db.Dialector.Name() == "postgres" {
		return "strpos(hostname, ?) > 0"
	}
	return "instr(hostname, ?) > 0"
}

// apply adds the shared filters (trash exclusion + status + technology + failed
// + review) to a query and returns it. Callers must use the returned value.
func (f galleryFilters) apply(query *gorm.DB, h *ApiHandler) *gorm.DB {
	query = query.Where(trashExclusionClause(h.DB))

	if f.statusClause != "" {
		query = query.Where(f.statusClause, f.statusArgs...)
	}

	if len(f.technologies) > 0 {
		query = query.Where("id in (?)", h.DB.Model(&models.Technology{}).
			Select("result_id").Distinct("result_id").
			Where("value IN (?)", f.technologies))
	}

	if !f.showFailed {
		query = query.Where("failed = ?", false)
	}

	// Category filter: restrict to results whose root_domain is assigned to the
	// selected category.
	if f.categoryID > 0 {
		query = query.Where("root_domain IN (?)", h.DB.Model(&models.DomainCategory{}).
			Select("domain").Where("category_id = ?", f.categoryID))
	}

	switch f.reviewFilter {
	case "":
		// no review filter
	case "unseen":
		query = query.Where("id NOT IN (?)", h.DB.Model(&models.Review{}).
			Select("result_id").Where("status != ''"))
	case "commented":
		query = query.Where("id IN (?)", h.DB.Model(&models.Review{}).
			Select("result_id").Where("comment != ''"))
	default:
		query = query.Where("id IN (?)", h.DB.Model(&models.Review{}).
			Select("result_id").Where("status = ?", f.reviewFilter))
	}

	return query
}
