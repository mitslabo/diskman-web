package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func expandHome(path string) string {
	if len(path) > 1 && path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func userDataDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		dir := os.Getenv("AppData")
		if dir == "" {
			return "", os.ErrNotExist
		}
		return dir, nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	default:
		if dir := os.Getenv("XDG_DATA_HOME"); dir != "" && filepath.IsAbs(dir) {
			return dir, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share"), nil
	}
}

func defaultDataDir() string {
	if d, err := userDataDir(); err == nil {
		return filepath.Join(d, "diskman-web")
	}
	return "diskman-web"
}

func defaultDevices(grid [][]int) map[string]string {
	devices := map[string]string{}
	for _, row := range grid {
		for _, slot := range row {
			key := fmt.Sprintf("%d", slot)
			if _, ok := devices[key]; ok {
				continue
			}
			devices[key] = fmt.Sprintf("/dev/disk%d", slot)
		}
	}
	return devices
}

func DefaultConfig() Config {
	dataDir := defaultDataDir()
	return Config{
		Addr:    ":8080",
		LogFile: filepath.Join(dataDir, "jobs.jsonl"),
		MapDir:  filepath.Join(dataDir, "maps"),
		Enclosures: []Enclosure{
			{
				Name:    "2-bay",
				Rows:    2,
				Cols:    1,
				Grid:    [][]int{{1}, {2}},
				Devices: defaultDevices([][]int{{1}, {2}}),
			},
			{
				Name:    "4-bay",
				Rows:    4,
				Cols:    1,
				Grid:    [][]int{{1}, {2}, {3}, {4}},
				Devices: defaultDevices([][]int{{1}, {2}, {3}, {4}}),
			},
			{
				Name:    "6-bay",
				Rows:    6,
				Cols:    1,
				Grid:    [][]int{{1}, {2}, {3}, {4}, {5}, {6}},
				Devices: defaultDevices([][]int{{1}, {2}, {3}, {4}, {5}, {6}}),
			},
			{
				Name:    "8-bay",
				Rows:    4,
				Cols:    2,
				Grid:    [][]int{{1, 5}, {2, 6}, {3, 7}, {4, 8}},
				Devices: defaultDevices([][]int{{1, 5}, {2, 6}, {3, 7}, {4, 8}}),
			},
			{
				Name:    "12-bay",
				Rows:    3,
				Cols:    4,
				Grid:    [][]int{{1, 2, 3, 4}, {5, 6, 7, 8}, {9, 10, 11, 12}},
				Devices: defaultDevices([][]int{{1, 2, 3, 4}, {5, 6, 7, 8}, {9, 10, 11, 12}}),
			},
		},
	}
}

// Load returns config. If configSpecified=true, missing file is an error.
func Load(path string, configSpecified bool) (Config, error) {
	defaultCfg := DefaultConfig()
	if path == "" {
		return defaultCfg, nil
	}
	path = expandHome(path)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if !configSpecified {
				return defaultCfg, nil
			}
			return Config{}, err
		}
		return Config{}, err
	}
	var userCfg Config
	if err := json.Unmarshal(b, &userCfg); err != nil {
		return Config{}, err
	}
	if len(userCfg.Enclosures) == 0 {
		userCfg.Enclosures = defaultCfg.Enclosures
	}
	for _, e := range userCfg.Enclosures {
		if err := e.Validate(); err != nil {
			return Config{}, err
		}
	}
	if userCfg.ActiveEnclosure != "" {
		found := false
		for _, e := range userCfg.Enclosures {
			if e.Name == userCfg.ActiveEnclosure {
				found = true
				break
			}
		}
		if !found {
			return Config{}, fmt.Errorf("activeEnclosure '%s' not found in enclosures", userCfg.ActiveEnclosure)
		}
	}
	if userCfg.LogFile != "" {
		userCfg.LogFile = expandHome(userCfg.LogFile)
	} else {
		userCfg.LogFile = defaultCfg.LogFile
	}
	if userCfg.MapDir != "" {
		userCfg.MapDir = expandHome(userCfg.MapDir)
	} else {
		userCfg.MapDir = defaultCfg.MapDir
	}
	if userCfg.Addr == "" {
		userCfg.Addr = defaultCfg.Addr
	}
	return userCfg, nil
}

// Save writes config to path, creating parent directories if needed.
func Save(path string, cfg Config) error {
	if path == "" {
		return errors.New("config path is required")
	}
	path = expandHome(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}
