package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func (s *Server) inspectRepository(w http.ResponseWriter, r *http.Request) {
	var input struct {
		URL string `json:"url"`
	}
	if !decodeRequest(w, r, &input) {
		return
	}
	if _, _, err := parseRepositoryURL(input.URL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := randomID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not start repository inspection.")
		return
	}
	request := inspectionRequest{ID: id, URL: input.URL}
	job := inspectionResponse{ID: request.ID, Status: "queued", Step: "queued"}
	s.mu.Lock()
	select {
	case s.inspectionQueue <- request:
		s.inspections[request.ID] = job
		s.mu.Unlock()
		writeJSON(w, http.StatusAccepted, job)
	default:
		s.mu.Unlock()
		writeError(w, http.StatusServiceUnavailable, "Repository inspection is busy. Try again shortly.")
	}
}

func (s *Server) inspectionStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	job, exists := s.inspections[id]
	s.mu.Unlock()
	if !exists {
		writeError(w, http.StatusNotFound, "Repository inspection not found.")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) inspectionWorker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case input := <-s.inspectionQueue:
			ctx, cancel := context.WithTimeout(s.ctx, inspectionTimeout)
			repo, workspace, err := s.inspect(ctx, input)
			timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded)
			cancel()
			if err != nil {
				if cleanupErr := removeWorkspace(workspace); cleanupErr != nil {
					err = errors.Join(err, fmt.Errorf("remove temporary workspace: %w", cleanupErr))
				}
				step := s.inspectionStep(input.ID)
				message := err.Error()
				if timedOut {
					message = fmt.Sprintf("Repository inspection timed out after five minutes during %s.", inspectionStepLabel(step))
				}
				s.updateInspection(inspectionResponse{ID: input.ID, Status: "error", Step: step, Error: message})
			} else {
				s.mu.Lock()
				s.inspections[input.ID] = inspectionResponse{ID: input.ID, Status: "complete", Repository: &repo}
				s.inspectionDirs[input.ID] = workspace
				s.inspectionDone[input.ID] = time.Now()
				s.mu.Unlock()
			}
		}
	}
}

func (s *Server) updateInspection(job inspectionResponse) {
	s.mu.Lock()
	s.inspections[job.ID] = job
	if job.Status == "error" {
		s.inspectionDone[job.ID] = time.Now()
	}
	s.mu.Unlock()
}

func (s *Server) inspectionStep(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inspections[id].Step
}

func inspectionStepLabel(step string) string {
	switch step {
	case "resolving":
		return "commit lookup"
	case "downloading":
		return "source download"
	case "discovering":
		return "Go module discovery"
	default:
		return "repository inspection"
	}
}

func (s *Server) inspect(ctx context.Context, input inspectionRequest) (Repository, string, error) {
	s.updateInspection(inspectionResponse{ID: input.ID, Status: "running", Step: "resolving"})
	repo, err := s.github.resolve(ctx, input.URL)
	if err != nil {
		return repo, "", err
	}
	workspace, err := os.MkdirTemp("", "govisualize-")
	if err != nil {
		return repo, "", err
	}
	s.updateInspection(inspectionResponse{ID: input.ID, Status: "running", Step: "downloading"})
	dir := filepath.Join(workspace, "repo")
	var downloaded, total int64
	err = s.github.download(ctx, repo, dir, func(progressDownloaded, progressTotal int64) {
		if progressTotal < 0 {
			progressTotal = 0
		}
		downloaded = progressDownloaded
		total = progressTotal
		s.updateInspection(inspectionResponse{ID: input.ID, Status: "running", Step: "downloading", Downloaded: downloaded, Total: total})
	})
	if err != nil {
		return repo, workspace, err
	}
	s.updateInspection(inspectionResponse{ID: input.ID, Status: "running", Step: "discovering", Downloaded: downloaded, Total: total})
	repo.Modules, err = discoverModules(dir)
	if err != nil {
		return repo, workspace, err
	}
	if len(repo.Modules) == 0 {
		return repo, workspace, fmt.Errorf("no Go modules found in this repository")
	}
	return repo, workspace, nil
}
