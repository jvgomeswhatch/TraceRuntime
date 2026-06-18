package http

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type CapacityHandler struct {
	resultsDir string
}

func NewCapacityHandler(resultsDir string) *CapacityHandler {
	return &CapacityHandler{resultsDir: resultsDir}
}

func (h *CapacityHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(h.resultsDir)
	if err != nil {
		jsonError(w, "no capacity results available", http.StatusNotFound)
		return
	}

	var jsonFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "loadtest-") && strings.HasSuffix(e.Name(), ".json") {
			jsonFiles = append(jsonFiles, e.Name())
		}
	}

	if len(jsonFiles) == 0 {
		jsonError(w, "no capacity results available", http.StatusNotFound)
		return
	}

	sort.Strings(jsonFiles)
	latest := jsonFiles[len(jsonFiles)-1]

	data, err := os.ReadFile(filepath.Join(h.resultsDir, latest))
	if err != nil {
		jsonError(w, "failed to read results", http.StatusInternalServerError)
		return
	}

	var report json.RawMessage
	if err := json.Unmarshal(data, &report); err != nil {
		jsonError(w, "invalid results file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
