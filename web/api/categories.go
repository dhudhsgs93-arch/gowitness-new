package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/sensepost/gowitness/pkg/log"
	"github.com/sensepost/gowitness/pkg/models"
	"gorm.io/gorm/clause"
)

type categoryResponse struct {
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	Color       string    `json:"color"`
	CreatedAt   time.Time `json:"created_at"`
	DomainCount int64     `json:"domain_count"`
	HostCount   int64     `json:"host_count"`
}

type categoryCreateRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type categoryDeleteRequest struct {
	ID uint `json:"id"`
}

type domainResponse struct {
	Domain        string `json:"domain"`
	Hosts         int64  `json:"hosts"`
	CategoryID    uint   `json:"category_id"`
	CategoryName  string `json:"category_name"`
	CategoryColor string `json:"category_color"`
}

type assignRequest struct {
	Domains    []string `json:"domains"`
	CategoryID uint     `json:"category_id"`
}

type unassignRequest struct {
	Domains []string `json:"domains"`
}

// CategoryListHandler lists all categories with their domain and host counts.
//
//	@Summary		List categories
//	@Description	List all host categories with domain and (distinct) host counts.
//	@Tags			Categories
//	@Produce		json
//	@Success		200	{array}	categoryResponse
//	@Router			/categories [get]
func (h *ApiHandler) CategoryListHandler(w http.ResponseWriter, r *http.Request) {
	var cats []models.Category
	h.DB.Order("name ASC").Find(&cats)

	// domain counts per category, in one grouped query
	type countRow struct {
		CategoryID uint
		N          int64
	}
	domainCounts := map[uint]int64{}
	{
		var rows []countRow
		h.DB.Model(&models.DomainCategory{}).
			Select("category_id, COUNT(*) as n").
			Group("category_id").Scan(&rows)
		for _, row := range rows {
			domainCounts[row.CategoryID] = row.N
		}
	}

	// distinct-host counts per category: join the assigned domains back to
	// results on root_domain and count distinct hostnames.
	hostCounts := map[uint]int64{}
	{
		var rows []countRow
		h.DB.Table("domain_categories dc").
			Joins("JOIN results r ON r.root_domain = dc.domain").
			Select("dc.category_id as category_id, COUNT(DISTINCT r.hostname) as n").
			Group("dc.category_id").Scan(&rows)
		for _, row := range rows {
			hostCounts[row.CategoryID] = row.N
		}
	}

	resp := make([]categoryResponse, 0, len(cats))
	for _, c := range cats {
		resp = append(resp, categoryResponse{
			ID:          c.ID,
			Name:        c.Name,
			Color:       c.Color,
			CreatedAt:   c.CreatedAt,
			DomainCount: domainCounts[c.ID],
			HostCount:   hostCounts[c.ID],
		})
	}

	json.NewEncoder(w).Encode(resp)
}

// CategoryCreateHandler creates a new category.
//
//	@Summary		Create category
//	@Description	Create a new host category.
//	@Tags			Categories
//	@Accept			json
//	@Produce		json
//	@Param			body	body		categoryCreateRequest	true	"Category data"
//	@Success		200		{object}	models.Category
//	@Router			/categories [post]
func (h *ApiHandler) CategoryCreateHandler(w http.ResponseWriter, r *http.Request) {
	var req categoryCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}

	color := strings.TrimSpace(req.Color)
	if color == "" {
		color = "#6b7280" // neutral gray fallback
	}

	var existing models.Category
	if h.DB.Where("name = ?", name).First(&existing).RowsAffected > 0 {
		http.Error(w, `{"error":"a category with that name already exists"}`, http.StatusConflict)
		return
	}

	cat := models.Category{Name: name, Color: color, CreatedAt: time.Now()}
	if err := h.DB.Create(&cat).Error; err != nil {
		log.Error("could not create category", "err", err)
		http.Error(w, `{"error":"db error"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(cat)
}

// CategoryDeleteHandler deletes a category and all of its domain assignments.
//
//	@Summary		Delete category
//	@Description	Delete a category and unassign all of its domains.
//	@Tags			Categories
//	@Accept			json
//	@Produce		json
//	@Param			body	body		categoryDeleteRequest	true	"Category id"
//	@Success		200		{object}	map[string]bool
//	@Router			/categories/delete [post]
func (h *ApiHandler) CategoryDeleteHandler(w http.ResponseWriter, r *http.Request) {
	var req categoryDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.ID == 0 {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}

	// Remove the assignments first, then the category itself.
	h.DB.Where("category_id = ?", req.ID).Delete(&models.DomainCategory{})
	if err := h.DB.Delete(&models.Category{}, req.ID).Error; err != nil {
		log.Error("could not delete category", "err", err)
		http.Error(w, `{"error":"db error"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// CategoryDomainsHandler lists every registrable domain seen in results, with
// its distinct-host count and current category assignment (if any).
//
//	@Summary		List domains
//	@Description	List all root domains with host counts and category assignment.
//	@Tags			Categories
//	@Produce		json
//	@Param			q	query	string	false	"Filter domains by substring."
//	@Success		200	{array}	domainResponse
//	@Router			/categories/domains [get]
func (h *ApiHandler) CategoryDomainsHandler(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))

	type domainRow struct {
		Domain string
		Hosts  int64
	}
	var rows []domainRow
	query := h.DB.Model(&models.Result{}).
		Select("root_domain as domain, COUNT(DISTINCT hostname) as hosts").
		Where("root_domain != ''").
		Group("root_domain").
		Order("hosts DESC")
	if q != "" {
		query = query.Where("root_domain LIKE ?", "%"+q+"%")
	}
	query.Scan(&rows)

	// map domain -> assigned category
	type assignRow struct {
		Domain        string
		CategoryID    uint
		CategoryName  string
		CategoryColor string
	}
	assigns := map[string]assignRow{}
	{
		var ar []assignRow
		h.DB.Table("domain_categories dc").
			Joins("JOIN categories c ON c.id = dc.category_id").
			Select("dc.domain, dc.category_id, c.name as category_name, c.color as category_color").
			Scan(&ar)
		for _, a := range ar {
			assigns[a.Domain] = a
		}
	}

	resp := make([]domainResponse, 0, len(rows))
	for _, row := range rows {
		d := domainResponse{Domain: row.Domain, Hosts: row.Hosts}
		if a, ok := assigns[row.Domain]; ok {
			d.CategoryID = a.CategoryID
			d.CategoryName = a.CategoryName
			d.CategoryColor = a.CategoryColor
		}
		resp = append(resp, d)
	}

	json.NewEncoder(w).Encode(resp)
}

// CategoryAssignHandler assigns a set of domains to a category (upsert).
//
//	@Summary		Assign domains
//	@Description	Assign a set of domains to a category, overwriting any prior assignment.
//	@Tags			Categories
//	@Accept			json
//	@Produce		json
//	@Param			body	body		assignRequest	true	"Domains and target category"
//	@Success		200		{object}	map[string]interface{}
//	@Router			/categories/assign [post]
func (h *ApiHandler) CategoryAssignHandler(w http.ResponseWriter, r *http.Request) {
	var req assignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.CategoryID == 0 || len(req.Domains) == 0 {
		http.Error(w, `{"error":"category_id and domains are required"}`, http.StatusBadRequest)
		return
	}

	if h.DB.First(&models.Category{}, req.CategoryID).RowsAffected == 0 {
		http.Error(w, `{"error":"category not found"}`, http.StatusNotFound)
		return
	}

	count := 0
	for _, raw := range req.Domains {
		domain := strings.ToLower(strings.TrimSpace(raw))
		if domain == "" {
			continue
		}
		// Upsert on the unique domain column so re-assigning moves the domain
		// to the new category rather than tripping the unique index.
		assignment := models.DomainCategory{Domain: domain, CategoryID: req.CategoryID}
		if err := h.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "domain"}},
			DoUpdates: clause.AssignmentColumns([]string{"category_id"}),
		}).Create(&assignment).Error; err != nil {
			log.Error("could not assign domain", "domain", domain, "err", err)
			continue
		}
		count++
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "count": count})
}

// CategoryUnassignHandler removes the category assignment for a set of domains.
//
//	@Summary		Unassign domains
//	@Description	Remove the category assignment for a set of domains.
//	@Tags			Categories
//	@Accept			json
//	@Produce		json
//	@Param			body	body		unassignRequest	true	"Domains to unassign"
//	@Success		200		{object}	map[string]interface{}
//	@Router			/categories/unassign [post]
func (h *ApiHandler) CategoryUnassignHandler(w http.ResponseWriter, r *http.Request) {
	var req unassignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if len(req.Domains) == 0 {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "count": 0})
		return
	}

	domains := make([]string, 0, len(req.Domains))
	for _, raw := range req.Domains {
		if d := strings.ToLower(strings.TrimSpace(raw)); d != "" {
			domains = append(domains, d)
		}
	}

	res := h.DB.Where("domain IN ?", domains).Delete(&models.DomainCategory{})
	if res.Error != nil {
		log.Error("could not unassign domains", "err", res.Error)
		http.Error(w, `{"error":"db error"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "count": res.RowsAffected})
}
