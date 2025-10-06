package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"
)

type Step struct {
	Program string   `json:"program"` // must match a key in Config.Programs
	Args    []string `json:"args"`
}

type Preset struct {
	Name  string `json:"name"`
	Steps []Step `json:"steps"`
}

type presetStore struct {
	file string
	mu   sync.RWMutex
	list map[string]Preset // name -> preset
}

var presets presetStore

func initPresetStore(file string) error {
	presets = presetStore{file: file, list: map[string]Preset{}}

	b, err := os.ReadFile(file)
	if err != nil {
		return savePresets()
	}

	var arr []Preset
	if err := json.Unmarshal(b, &arr); err != nil {
		logger.Warn("Failed to parse presets file, starting empty", zap.Error(err))
		return savePresets()
	}

	for _, p := range arr {
		presets.list[p.Name] = p
	}

	return nil
}

func savePresets() error {
	presets.mu.RLock()
	defer presets.mu.RUnlock()

	arr := make([]Preset, 0, len(presets.list))

	for _, p := range presets.list {
		arr = append(arr, p)
	}

	b, err := json.MarshalIndent(arr, "", "  ")
	if err != nil {
		return err
	}

	_ = os.MkdirAll(filepath.Dir(presets.file), 0755)

	return os.WriteFile(presets.file, b, 0644)
}

func setPreset(p Preset) error {
	if p.Name == "" {
		return errors.New("preset name required")
	}

	// validate steps against config programs
	for _, s := range p.Steps {
		if _, ok := config.Programs[s.Program]; !ok {
			return errors.New("unknown program: " + s.Program)
		}
	}

	presets.mu.Lock()
	presets.list[p.Name] = p
	presets.mu.Unlock()

	return savePresets()
}

func getAllPresets() []Preset {
	presets.mu.RLock()
	defer presets.mu.RUnlock()

	arr := make([]Preset, 0, len(presets.list))

	for _, p := range presets.list {
		arr = append(arr, p)
	}

	return arr
}

func handleListPresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(getAllPresets())
}

func handleCreateOrUpdatePreset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := CheckPassword(r.Header.Get("X-Password")); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var p Preset

	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if err := setPreset(p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

	_ = json.NewEncoder(w).Encode(p)
}
