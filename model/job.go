package model

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type JobState string

const (
	JobPending   JobState = "pending"
	JobRunning   JobState = "running"
	JobDone      JobState = "done"
	JobError     JobState = "error"
	JobCancelled JobState = "cancelled"
)

type Progress struct {
	Pass      int     `json:"pass"`
	Percent   float64 `json:"percent"`
	Rescued   string  `json:"rescued"`
	Rate      string  `json:"rate"`
	Remaining string  `json:"remaining"`
	BadAreas  int     `json:"badAreas"`
	ReadErrs  int     `json:"readErrs"`
}

type Job struct {
	ID        string   `json:"id"`
	Op        string   `json:"op"`
	Name      string   `json:"name"`
	Src       string   `json:"src"`
	Dst       string   `json:"dst"`
	MapFile   string   `json:"mapFile"`
	State     JobState `json:"state"`
	Progress  Progress `json:"progress"`
	ErrMsg    string   `json:"errMsg"`
	CreatedAt time.Time `json:"createdAt"`
}

func NewJobID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return time.Now().Format("20060102150405")
	}
	return hex.EncodeToString(b)
}
