package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/sensepost/gowitness/pkg/log"
	"github.com/sensepost/gowitness/pkg/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// detailResponse is a result plus the ids of the adjacent results, so the UI
// can offer prev/next navigation that lands on real results (ids are not
// contiguous once anything is deleted or a scan skips ports).
type detailResponse struct {
	*models.Result
	PrevID *uint `json:"prev_id"`
	NextID *uint `json:"next_id"`
}

// DetailHandler returns the detail for a screenshot
//
//	@Summary		Results detail
//	@Description	Get details for a result.
//	@Tags			Results
//	@Accept			json
//	@Produce		json
//	@Param			id	path		int	true	"The screenshot ID to load."
//	@Success		200	{object}	models.Result
//	@Router			/results/detail/{id} [get]
func (h *ApiHandler) DetailHandler(w http.ResponseWriter, r *http.Request) {
	var response models.Result

	err := h.DB.Model(&models.Result{}).
		Preload(clause.Associations).
		Preload("TLS.SanList").
		Preload("Review").
		First(&response, chi.URLParam(r, "id")).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Return a real 404 with a JSON body instead of an empty 200, which
		// the frontend would choke on ("Unexpected end of JSON input").
		http.Error(w, `{"error":"result not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		log.Error("could not get detail for id", "err", err)
		http.Error(w, `{"error":"db error"}`, http.StatusInternalServerError)
		return
	}

	// resolve the actual adjacent result ids (not id±1)
	out := detailResponse{Result: &response}
	var neighbour models.Result
	if h.DB.Model(&models.Result{}).Select("id").
		Where("id < ?", response.ID).Order("id DESC").First(&neighbour).Error == nil {
		id := neighbour.ID
		out.PrevID = &id
	}
	neighbour = models.Result{}
	if h.DB.Model(&models.Result{}).Select("id").
		Where("id > ?", response.ID).Order("id ASC").First(&neighbour).Error == nil {
		id := neighbour.ID
		out.NextID = &id
	}

	jsonData, err := json.Marshal(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(jsonData)
}
