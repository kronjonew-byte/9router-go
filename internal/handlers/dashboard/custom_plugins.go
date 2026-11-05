package dashboard

import (
	"io"
	"net/http"

	json "9router/proxy/internal/fastjson"
	"9router/proxy/internal/handlerutil"
)

// HandleGetCustomPlugins handles GET /api/custom-plugins.
// Returns the persisted plugin config map merged over the defaults, so a fresh
// install answers with every plugin present and disabled rather than an empty
// body the dashboard would have to interpret.
func (h *DashboardHandler) HandleGetCustomPlugins(w http.ResponseWriter, r *http.Request) {
	persisted := h.Repo.GetCustomPlugins()
	if persisted == nil {
		persisted = map[string]any{}
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"customPlugins": persisted})
}

// HandlePutCustomPlugins handles PUT /api/custom-plugins.
// Persists the whole plugin config map. The body is a bare JSON object of
// plugin id -> {enabled, models, level}; it is written through unchanged so the
// dashboard owns the validation and the gateway stays a dumb store.
func (h *DashboardHandler) HandlePutCustomPlugins(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	defer r.Body.Close()

	var cfg map[string]any
	if err := json.Unmarshal(body, &cfg); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	if err := h.Repo.SetCustomPlugins(cfg); err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"customPlugins": cfg})
}
