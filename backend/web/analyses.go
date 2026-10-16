package web

import (
	"compress/gzip"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"govisualize/analyzer"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

func validAnalysisRequest(input analysisRequest) bool {
	return validJobID(input.InspectionID) && ownerPattern.MatchString(input.Owner) && repoPattern.MatchString(input.Repo) && input.Repo != "." && input.Repo != ".." && shaPattern.MatchString(input.SHA) && validModule(input.Module)
}

func (s *Server) startAnalysis(w http.ResponseWriter, r *http.Request) {
	var input analysisRequest
	if !decodeRequest(w, r, &input) {
		return
	}
	input.Owner = strings.ToLower(input.Owner)
	input.Repo = strings.ToLower(input.Repo)
	if !validAnalysisRequest(input) {
		writeError(w, http.StatusBadRequest, "Invalid repository, commit, or module.")
		return
	}
	id, err := randomID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not start analysis.")
		return
	}
	s.mu.Lock()
	inspection, exists := s.inspections[input.InspectionID]
	workspace := s.inspectionDirs[input.InspectionID]
	if !exists || inspection.Status != "complete" || inspection.Repository == nil || workspace == "" {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "Repository inspection expired or was already used. Inspect the repository again.")
		return
	}
	repo := inspection.Repository
	if repo.Owner != input.Owner || repo.Repo != input.Repo || repo.SHA != input.SHA || !slices.Contains(repo.Modules, input.Module) {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "Repository, commit, or module does not match the inspection.")
		return
	}
	input.id = id
	input.workspace = workspace
	job := jobResponse{ID: id, Status: "queued", Step: "queued"}
	select {
	case s.queue <- input:
		delete(s.inspectionDirs, input.InspectionID)
		s.analysisDirs[id] = workspace
		s.jobs[id] = job
		s.inputs[id] = input
		s.mu.Unlock()
		writeJSON(w, http.StatusAccepted, job)
	default:
		s.mu.Unlock()
		writeError(w, http.StatusServiceUnavailable, "Analysis queue is full. Try again shortly.")
	}
}

func validJobID(id string) bool {
	if len(id) != 64 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (s *Server) analysisStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validJobID(id) {
		writeError(w, http.StatusNotFound, "Analysis not found.")
		return
	}
	s.mu.Lock()
	job, exists := s.jobs[id]
	s.mu.Unlock()
	if !exists {
		writeError(w, http.StatusNotFound, "Analysis not found.")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) graph(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validJobID(id) {
		writeError(w, http.StatusNotFound, "Graph not found.")
		return
	}
	s.mu.Lock()
	input, exists := s.inputs[id]
	job := s.jobs[id]
	data, available := s.graphs[id]
	if !exists || job.Status != "complete" || !available {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "Graph not found.")
		return
	}
	file, err := os.Open(data.path)
	s.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusNotFound, "Graph is no longer available. Analyze the repository again.")
		return
	}
	defer file.Close()
	ctx, cancel := context.WithTimeout(r.Context(), githubCheckTimeout)
	defer cancel()
	if _, err := publicRepositoryHead(ctx, input.Owner, input.Repo); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			writeError(w, http.StatusServiceUnavailable, "GitHub access check timed out after 45 seconds. Try again.")
			return
		}
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	reader, err := gzip.NewReader(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read the graph. Analyze the repository again.")
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.FormatInt(data.size, 10))
	io.Copy(w, reader)
}

func (s *Server) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case input := <-s.queue:
			ctx, cancel := context.WithTimeout(s.ctx, analysisTimeout)
			data, diagnostics, err := s.analyze(ctx, input)
			timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded)
			cancel()
			step := s.analysisStep(input.id)
			s.updateJob(jobResponse{ID: input.id, Status: "running", Step: "cleanup"})
			if cleanupErr := removeWorkspace(input.workspace); cleanupErr != nil {
				if err == nil {
					step = "cleanup"
				}
				err = errors.Join(err, fmt.Errorf("remove temporary workspace: %w", cleanupErr))
			}
			s.mu.Lock()
			delete(s.analysisDirs, input.id)
			s.mu.Unlock()
			if err != nil {
				if data.path != "" {
					err = errors.Join(err, os.Remove(data.path))
				}
				message := err.Error()
				if timedOut {
					message = fmt.Sprintf("Analysis timed out after twenty minutes during %s.", analysisStepLabel(step))
				}
				s.updateJob(jobResponse{ID: input.id, Status: "error", Step: step, Error: message})
			} else {
				s.mu.Lock()
				s.graphs[input.id] = data
				s.jobs[input.id] = jobResponse{ID: input.id, Status: "complete", Step: "complete", Diagnostics: diagnostics}
				s.jobFinished[input.id] = time.Now()
				s.mu.Unlock()
			}
		}
	}
}

func (s *Server) updateJob(job jobResponse) {
	s.mu.Lock()
	s.jobs[job.ID] = job
	if job.Status == "error" {
		s.jobFinished[job.ID] = time.Now()
	}
	s.mu.Unlock()
}

func (s *Server) analysisStep(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobs[id].Step
}

func analysisStepLabel(step string) string {
	switch step {
	case "preparing":
		return "module preparation"
	case "loading":
		return "Go source parsing"
	case "mapping":
		return "call graph mapping"
	case "saving":
		return "graph encoding"
	case "cleanup":
		return "temporary workspace cleanup"
	default:
		return "analysis"
	}
}

func (s *Server) analyze(ctx context.Context, input analysisRequest) (storedGraph, []string, error) {
	s.updateJob(jobResponse{ID: input.id, Status: "running", Step: "preparing"})
	moduleDir := filepath.Join(input.workspace, "repo", filepath.FromSlash(input.Module))
	if _, err := os.Stat(filepath.Join(moduleDir, "go.mod")); err != nil {
		return storedGraph{}, nil, fmt.Errorf("Go module not found at %s", input.Module)
	}
	graph, diagnostics, err := analyzer.AnalyzeModule(ctx, moduleDir, func(step string) {
		s.updateJob(jobResponse{ID: input.id, Status: "running", Step: step})
	})
	if err != nil {
		return storedGraph{}, nil, err
	}
	s.updateJob(jobResponse{ID: input.id, Status: "running", Step: "saving"})
	data, err := saveGraph(ctx, graph)
	if err != nil {
		return storedGraph{}, nil, err
	}
	return data, diagnostics, nil
}
