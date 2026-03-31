package web

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"diskman-web/config"
	"diskman-web/model"
	"diskman-web/runner"
)

// Server holds all application state.
type Server struct {
	cfg        config.Config
	configPath string
	dryRun     bool
	debug      bool

	activeEnclosureIdx  int
	activeEnclosureName string
	needsSetup          bool // true when no enclosure is selected yet

	mu         sync.RWMutex
	jobs       map[string]*model.Job
	jobOrder   []string
	jobCancels map[string]context.CancelFunc
	updates    chan runner.Update

	// SSE broadcast
	sseClients map[chan []byte]struct{}
	sseMu      sync.Mutex
}

func NewServer(cfg config.Config, configPath string, enclosureName string, dryRun, debug bool) (*Server, error) {
	// Priority: CLI --enclosure > config.ActiveEnclosure
	activeName := enclosureName
	if activeName == "" {
		activeName = cfg.ActiveEnclosure
	}

	needsSetup := false
	activeIdx := -1
	if activeName != "" {
		for i, e := range cfg.Enclosures {
			if e.Name == activeName {
				activeIdx = i
				break
			}
		}
		if activeIdx < 0 {
			return nil, fmt.Errorf("enclosure '%s' not found in config", activeName)
		}
	} else {
		needsSetup = true
		activeIdx = 0
		if len(cfg.Enclosures) > 0 {
			activeName = cfg.Enclosures[0].Name
		}
	}

	s := &Server{
		cfg:                 cfg,
		configPath:          configPath,
		dryRun:              dryRun,
		debug:               debug,
		activeEnclosureIdx:  activeIdx,
		activeEnclosureName: activeName,
		needsSetup:          needsSetup,
		jobs:                make(map[string]*model.Job),
		jobCancels:          make(map[string]context.CancelFunc),
		updates:             make(chan runner.Update, 256),
		sseClients:          make(map[chan []byte]struct{}),
	}
	go s.processUpdates()
	return s, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", serveIndex)
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("POST /api/jobs", s.handleStartJob)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.handleCancelJob)
	mux.HandleFunc("GET /api/diskinfo", s.handleDiskInfo)
	mux.HandleFunc("GET /api/events", s.handleSSE)
	mux.HandleFunc("POST /api/enclosure", s.handleSetEnclosure)
	return mux
}

// ----- SSE broadcast -----

func (s *Server) addSSEClient() chan []byte {
	ch := make(chan []byte, 64)
	s.sseMu.Lock()
	s.sseClients[ch] = struct{}{}
	s.sseMu.Unlock()
	return ch
}

func (s *Server) removeSSEClient(ch chan []byte) {
	s.sseMu.Lock()
	delete(s.sseClients, ch)
	s.sseMu.Unlock()
}

func (s *Server) broadcast(data []byte) {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	for ch := range s.sseClients {
		select {
		case ch <- data:
		default:
		}
	}
}

func (s *Server) broadcastState() {
	payload, err := json.Marshal(s.buildState())
	if err != nil {
		return
	}
	msg := append([]byte("data: "), payload...)
	msg = append(msg, '\n', '\n')
	s.broadcast(msg)
}

// ----- update loop -----

func (s *Server) processUpdates() {
	for u := range s.updates {
		s.mu.Lock()
		if j, ok := s.jobs[u.JobID]; ok {
			j.Progress = u.Progress
			j.State = u.State
			if u.Err != nil {
				j.ErrMsg = u.Err.Error()
			}
			if u.Completed || u.Cancelled || u.State == model.JobError {
				delete(s.jobCancels, u.JobID)
			}
		}
		s.mu.Unlock()
		s.broadcastState()
	}
}

// ----- state DTO -----

type StateDTO struct {
	DryRun     bool            `json:"dryRun"`
	Debug      bool            `json:"debug"`
	Enclosure  string          `json:"enclosure"`
	Jobs       []*model.Job    `json:"jobs"`
	ActiveJobs []*model.Job    `json:"activeJobs"`
}

func (s *Server) buildState() StateDTO {
	s.mu.RLock()
	defer s.mu.RUnlock()

	all := make([]*model.Job, 0, len(s.jobOrder))
	active := make([]*model.Job, 0)
	for _, id := range s.jobOrder {
		j := s.jobs[id]
		if j == nil {
			continue
		}
		cp := *j
		all = append(all, &cp)
		if j.State == model.JobPending || j.State == model.JobRunning {
			active = append(active, &cp)
		}
	}
	return StateDTO{
		DryRun:     s.dryRun,
		Debug:      s.debug,
		Enclosure:  s.activeEnclosureName,
		Jobs:       all,
		ActiveJobs: active,
	}
}

type ConfigDTO struct {
	Addr                string             `json:"addr"`
	DryRun              bool               `json:"dryRun"`
	Debug               bool               `json:"debug"`
	Enclosures          []config.Enclosure `json:"enclosures"`
	ActiveEnclosureIdx  int                `json:"activeEnclosureIdx"`
	ActiveEnclosureName string             `json:"activeEnclosureName"`
	NeedsSetup          bool               `json:"needsSetup"`
}

// ----- HTTP handlers -----

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ConfigDTO{
		Addr:                s.cfg.Addr,
		DryRun:              s.dryRun,
		Debug:               s.debug,
		Enclosures:          s.cfg.Enclosures,
		ActiveEnclosureIdx:  s.activeEnclosureIdx,
		ActiveEnclosureName: s.activeEnclosureName,
		NeedsSetup:          s.needsSetup,
	})
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.buildState())
}

type StartJobRequest struct {
	Op           string `json:"op"` // "copy" | "erase"
	EnclosureIdx int    `json:"enclosureIdx"`
	SrcSlot     int    `json:"srcSlot"`
	DstSlot     int    `json:"dstSlot"`
}

func (s *Server) handleStartJob(w http.ResponseWriter, r *http.Request) {
	var req StartJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	enclosureIdx := s.activeEnclosureIdx
	if enclosureIdx < 0 || enclosureIdx >= len(s.cfg.Enclosures) {
		http.Error(w, "invalid enclosure index", http.StatusBadRequest)
		return
	}
	e := s.cfg.Enclosures[enclosureIdx]
	src := s.devicePath(e, req.SrcSlot)
	dst := s.devicePath(e, req.DstSlot)
	if req.Op == "copy" && (src == "" || dst == "") {
		http.Error(w, "src/dst device path not configured", http.StatusBadRequest)
		return
	}
	if req.Op == "erase" && src == "" {
		http.Error(w, "target device path not configured", http.StatusBadRequest)
		return
	}

	// Busy check
	s.mu.RLock()
	srcBusy := s.isDeviceBusy(src)
	dstBusy := req.Op == "copy" && s.isDeviceBusy(dst)
	s.mu.RUnlock()
	if srcBusy || dstBusy {
		http.Error(w, "selected disk is in use by a running job", http.StatusConflict)
		return
	}
	if req.Op == "copy" && req.SrcSlot == req.DstSlot {
		http.Error(w, "src and dst must differ", http.StatusBadRequest)
		return
	}

	id := model.NewJobID()
	if req.Op == "erase" {
		dst = src
	}
	job := &model.Job{
		ID:        id,
		Op:        req.Op,
		Name:      e.Name,
		Src:       src,
		Dst:       dst,
		MapFile:   filepath.Join(s.cfg.MapDir, id+".map"),
		State:     model.JobPending,
		CreatedAt: time.Now(),
	}

	s.mu.Lock()
	s.jobs[id] = job
	s.jobOrder = append(s.jobOrder, id)
	ctx, cancel := context.WithCancel(context.Background())
	s.jobCancels[id] = cancel
	s.mu.Unlock()

	runner.StartJob(ctx, *job, s.dryRun, s.updates)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	cancel, ok := s.jobCancels[id]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "job not found or already finished", http.StatusNotFound)
		return
	}
	cancel()
	w.WriteHeader(http.StatusNoContent)
}

type DiskInfoResponse struct {
	Device string `json:"device"`
	Slot   string `json:"slot"`
	Model  string `json:"model"`
	Serial string `json:"serial"`
	Size   string `json:"size"`
}

func (s *Server) handleDiskInfo(w http.ResponseWriter, r *http.Request) {
	encIdxStr := r.URL.Query().Get("enc")
	slotStr := r.URL.Query().Get("slot")
	encIdx, err := strconv.Atoi(encIdxStr)
	if err != nil || encIdx < 0 || encIdx >= len(s.cfg.Enclosures) {
		http.Error(w, "invalid enc", http.StatusBadRequest)
		return
	}
	slot, err := strconv.Atoi(slotStr)
	if err != nil {
		http.Error(w, "invalid slot", http.StatusBadRequest)
		return
	}
	e := s.cfg.Enclosures[encIdx]
	path := s.devicePath(e, slot)
	model, serial, size := getDiskInfo(path)
	resp := DiskInfoResponse{
		Device: path,
		Slot:   fmt.Sprintf("Slot%02d", slot),
		Model:  orNA(model),
		Serial: orNA(serial),
		Size:   orNA(size),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send current state immediately on connect (reconnect対応)
	payload, _ := json.Marshal(s.buildState())
	fmt.Fprintf(w, "data: %s\n\n", payload)
	flusher.Flush()

	ch := s.addSSEClient()
	defer s.removeSSEClient(ch)

	// Keep-alive ticker
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			_, err := w.Write(msg)
			if err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// ----- helpers -----

func (s *Server) devicePath(e config.Enclosure, slot int) string {
	if p := e.Devices[fmt.Sprintf("%d", slot)]; p != "" {
		return p
	}
	if s.debug {
		return fmt.Sprintf("/dev/disk%d", slot)
	}
	return ""
}

func (s *Server) isDeviceBusy(path string) bool {
	if path == "" {
		return false
	}
	for _, j := range s.jobs {
		if j == nil {
			continue
		}
		if j.State != model.JobPending && j.State != model.JobRunning {
			continue
		}
		if j.Src == path || j.Dst == path {
			return true
		}
	}
	return false
}

func (s *Server) handleSetEnclosure(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, "bad request: name required", http.StatusBadRequest)
		return
	}
	idx := -1
	for i, e := range s.cfg.Enclosures {
		if e.Name == req.Name {
			idx = i
			break
		}
	}
	if idx < 0 {
		http.Error(w, fmt.Sprintf("enclosure '%s' not found", req.Name), http.StatusBadRequest)
		return
	}

	s.activeEnclosureIdx = idx
	s.activeEnclosureName = req.Name
	s.needsSetup = false

	// 設定ファイルに保存
	if s.configPath != "" {
		s.cfg.ActiveEnclosure = req.Name
		if err := config.Save(s.configPath, s.cfg); err != nil {
			// 保存失敗はログ出力のみ（動作は継続）
			fmt.Fprintf(os.Stderr, "config save error: %v\n", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"name": req.Name})
}

func getDiskInfo(devicePath string) (mdl string, serial string, size string) {
	realPath, err := filepath.EvalSymlinks(devicePath)
	if err != nil {
		realPath = devicePath
	}
	base := filepath.Base(realPath)
	sysRoot := filepath.Join("/sys/block", base)
	mdl = readSysfsValue(filepath.Join(sysRoot, "device", "model"))
	serial = readSysfsValue(filepath.Join(sysRoot, "device", "serial"))
	szStr := readSysfsValue(filepath.Join(sysRoot, "size"))
	if szStr != "" {
		sz, err := strconv.ParseInt(strings.TrimSpace(szStr), 10, 64)
		if err == nil {
			size = formatBytes(sz * 512)
		}
	}
	return
}

func readSysfsValue(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func formatBytes(b int64) string {
	const (
		KB = 1 << (10 * 1)
		MB = 1 << (10 * 2)
		GB = 1 << (10 * 3)
		TB = 1 << (10 * 4)
	)
	f := float64(b)
	switch {
	case b >= TB:
		return fmt.Sprintf("%.2f TB", f/float64(TB))
	case b >= GB:
		return fmt.Sprintf("%.2f GB", f/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.2f MB", f/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.2f KB", f/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func orNA(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
}

// RandomConfirmCode generates a 4-digit code.
func RandomConfirmCode() string {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "0000"
	}
	v := int(binary.BigEndian.Uint16(b[:])) % 10000
	return fmt.Sprintf("%04d", v)
}
