package config

import "errors"

// Config is the top-level configuration for diskman-web.
type Config struct {
	Addr       string      `json:"addr"`
	LogFile    string      `json:"logFile"`
	MapDir     string      `json:"mapDir"`
	ActiveEnclosure string  `json:"activeEnclosure,omitempty"`
	Enclosures []Enclosure `json:"enclosures"`
}

// Enclosure defines the visual slot layout and slot->device mapping.
type Enclosure struct {
	Name    string            `json:"name"`
	Rows    int               `json:"rows"`
	Cols    int               `json:"cols"`
	Grid    [][]int           `json:"grid"`
	Devices map[string]string `json:"devices"`
}

func (e Enclosure) Validate() error {
	if e.Name == "" {
		return errors.New("enclosure name is required")
	}
	if e.Rows <= 0 || e.Cols <= 0 {
		return errors.New("rows/cols must be > 0")
	}
	if len(e.Grid) != e.Rows {
		return errors.New("grid row count does not match rows")
	}
	for _, row := range e.Grid {
		if len(row) != e.Cols {
			return errors.New("grid col count does not match cols")
		}
	}
	return nil
}
