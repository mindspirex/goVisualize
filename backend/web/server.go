package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const maxGraphBytes = 128 << 20

const inspectionTimeout = 5 * time.Minute

const analysisTimeout = 20 * time.Minute

const githubCheckTimeout = 45 * time.Second

const finishedJobLifetime = 30 * time.Minute

type analysisRequest struct {
	InspectionID string `json:"inspectionId"`
	Owner        string `json:"owner"`
	Repo         string `json:"repo"`
	SHA          string `json:"sha"`
	Module       string `json:"module"`
	id           string
	workspace    string
}

type jobResponse struct {
	ID          string   `json:"id"`
	Status      string   `json:"status"`
	Step        string   `json:"step,omitempty"`
	Error       string   `json:"error,omitempty"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

type inspectionRequest struct {
	ID  string
	URL string
}

type inspectionResponse struct {
	ID         string      `json:"id"`
	Status     string      `json:"status"`
	Step       string      `json:"step,omitempty"`
	Downloaded int64       `json:"downloaded,omitempty"`
	Total      int64       `json:"total,omitempty"`
	Repository *Repository `json:"repository,omitempty"`
	Error      string      `json:"error,omitempty"`
}

type Server struct {
	github          *githubClient
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	queue           chan analysisRequest
	inspectionQueue chan inspectionRequest
	mu              sync.Mutex
	jobs            map[string]jobResponse
	jobFinished     map[string]time.Time
	graphs          map[string]storedGraph
	analysisDirs    map[string]string
	inspections     map[string]inspectionResponse
	inspectionDirs  map[string]string
	inspectionDone  map[string]time.Time
	inputs          map[string]analysisRequest
}

func NewServer() *Server {
	ctx, cancel := context.WithCancel(context.Background())
	server := &Server{
		github:          newGitHubClient(),
		ctx:             ctx,
		cancel:          cancel,
		queue:           make(chan analysisRequest, 8),
		inspectionQueue: make(chan inspectionRequest, 8),
		jobs:            map[string]jobResponse{},
		jobFinished:     map[string]time.Time{},
		graphs:          map[string]storedGraph{},
		analysisDirs:    map[string]string{},
		inspections:     map[string]inspectionResponse{},
		inspectionDirs:  map[string]string{},
		inspectionDone:  map[string]time.Time{},
		inputs:          map[string]analysisRequest{},
	}
	server.wg.Add(5)
	for range 2 {
		go server.worker()
		go server.inspectionWorker()
	}
	go server.cleanupWorker()
	return server
}

func (s *Server) Close() {
	s.cancel()
	s.wg.Wait()
	s.mu.Lock()
	directories := make([]string, 0, len(s.inspectionDirs)+len(s.analysisDirs))
	for _, dir := range s.inspectionDirs {
		directories = append(directories, dir)
	}
	for _, dir := range s.analysisDirs {
		directories = append(directories, dir)
	}
	s.mu.Unlock()
	for _, dir := range directories {
		removeWorkspace(dir)
	}
	for _, graph := range s.graphs {
		os.Remove(graph.path)
	}
}

func removeWorkspace(root string) error {
	if root == "" {
		return nil
	}
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		return os.Chmod(path, info.Mode().Perm()|0700)
	}); err != nil {
		return err
	}
	return os.RemoveAll(root)
}

func (s *Server) cleanupWorker() {
	defer s.wg.Done()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.pruneFinished()
		}
	}
}

func (s *Server) pruneFinished() {
	cutoff := time.Now().Add(-finishedJobLifetime)
	var directories []string
	var files []string
	s.mu.Lock()
	for id, finished := range s.inspectionDone {
		if finished.After(cutoff) {
			continue
		}
		if dir := s.inspectionDirs[id]; dir != "" {
			directories = append(directories, dir)
			delete(s.inspectionDirs, id)
		}
		delete(s.inspections, id)
		delete(s.inspectionDone, id)
	}
	for id, finished := range s.jobFinished {
		if finished.After(cutoff) {
			continue
		}
		delete(s.jobs, id)
		delete(s.inputs, id)
		if graph, exists := s.graphs[id]; exists {
			files = append(files, graph.path)
		}
		delete(s.graphs, id)
		delete(s.jobFinished, id)
	}
	s.mu.Unlock()
	for _, dir := range directories {
		removeWorkspace(dir)
	}
	for _, file := range files {
		os.Remove(file)
	}
}

func randomID() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/repositories", s.inspectRepository)
	mux.HandleFunc("GET /api/repositories/{id}", s.inspectionStatus)
	mux.HandleFunc("POST /api/analyses", s.startAnalysis)
	mux.HandleFunc("GET /api/analyses/{id}", s.analysisStatus)
	mux.HandleFunc("GET /api/graphs/{id}", s.graph)
	return mux
}

func decodeRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "Expected one JSON object.")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
