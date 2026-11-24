package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
)

func SettingsPath() string { return filepath.Join(STATE, "settings.json") }

func SavePreference(key string, value any) error {
	unlock, err := DirLock(STATE)
	if err != nil {
		return err
	}
	defer unlock()
	path := SettingsPath()
	prefs := map[string]any{}
	b, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return err
	default:
		// Keep a file the user can repair instead of replacing every saved choice.
		if err := json.Unmarshal(b, &prefs); err != nil || prefs == nil {
			return errf("%s is not a valid settings file; fix or remove it: %v", path, err)
		}
	}
	prefs[key] = value
	return WriteJSON(path, prefs)
}

func DescriptionBudget() (int, string) {
	if n, err := strconv.Atoi(os.Getenv("SKILL_TOKEN_BUDGET")); err == nil && n > 0 {
		return n, "SKILL_TOKEN_BUDGET"
	}
	var prefs struct {
		Budget int `json:"token_budget"`
	}
	if LoadJSON(SettingsPath(), &prefs) && prefs.Budget > 0 {
		return prefs.Budget, "personal settings"
	}
	return 2000, "default"
}
