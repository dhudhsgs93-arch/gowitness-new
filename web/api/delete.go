package api

import (
	"encoding/json"
	"net/http"

	"github.com/sensepost/gowitness/pkg/log"
	"github.com/sensepost/gowitness/pkg/models"
)

type deleteResultRequest struct {
	ID int `json:"id"`
}

type deleteBulkRequest struct {
	IDs []int `json:"ids"`
}

// DeleteResultHandler deletes results from the database
//
//	@Summary		Delete a result
//	@Description	Deletes a result, by id, and all of its associated data from the database.
//	@Tags			Results
//	@Accept			json
//	@Produce		json
//	@Param			query	body		deleteResultRequest	true	"The result ID to delete"
//	@Success		200		{string}	string				"ok"
//	@Router			/results/delete [post]
func (h *ApiHandler) DeleteResultHandler(w http.ResponseWriter, r *http.Request) {
	var request deleteResultRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		log.Error("failed to read json request", "err", err)
		http.Error(w, "Error reading JSON request", http.StatusInternalServerError)
		return
	}

	log.Info("deleting id", "id", request.ID)

	if err := h.DB.Delete(&models.Result{}, request.ID).Error; err != nil {
		log.Error("failed to delete result", "err", err)
		return
	}

	response := `ok`
	jsonData, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "Error creating JSON response", http.StatusInternalServerError)
		return
	}

	w.Write(jsonData)
}

// DeleteBulkHandler deletes multiple results by IDs
func (h *ApiHandler) DeleteBulkHandler(w http.ResponseWriter, r *http.Request) {
	var request deleteBulkRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		log.Error("failed to read json request", "err", err)
		http.Error(w, "Error reading JSON request", http.StatusInternalServerError)
		return
	}

	if len(request.IDs) == 0 {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "count": 0})
		return
	}

	log.Info("bulk deleting", "count", len(request.IDs))

	if err := h.DB.Delete(&models.Result{}, request.IDs).Error; err != nil {
		log.Error("failed to bulk delete results", "err", err)
		http.Error(w, "Error deleting results", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "count": len(request.IDs)})
}
