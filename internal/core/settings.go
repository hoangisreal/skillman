package core

import (
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
	prefs := LoadMap(path)
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
