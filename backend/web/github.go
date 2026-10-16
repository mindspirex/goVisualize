package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var ownerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type Repository struct {
	Owner   string   `json:"owner"`
	Repo    string   `json:"repo"`
	SHA     string   `json:"sha"`
	Modules []string `json:"modules"`
}

type githubClient struct {
	http *http.Client
}

func newGitHubClient() *githubClient {
	return &githubClient{
		http: &http.Client{
			Timeout: 5 * time.Minute,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) > 3 || req.URL.Scheme != "https" || (req.URL.Hostname() != "github.com" && req.URL.Hostname() != "codeload.github.com") {
					return fmt.Errorf("unexpected GitHub redirect")
				}
				return nil
			},
		},
	}
}

func parseRepositoryURL(raw string) (string, string, error) {
	if !strings.Contains(raw, "://") {
		raw = "https://github.com/" + raw
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") || parsed.Port() != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", fmt.Errorf("enter a GitHub repository URL, such as https://github.com/owner/repo")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("enter a repository URL without a file or branch path")
	}
	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")
	if !ownerPattern.MatchString(owner) || !repoPattern.MatchString(repo) || repo == "." || repo == ".." {
		return "", "", fmt.Errorf("invalid GitHub repository name")
	}
	return strings.ToLower(owner), strings.ToLower(repo), nil
}

func (g *githubClient) resolve(ctx context.Context, rawURL string) (Repository, error) {
	owner, repo, err := parseRepositoryURL(rawURL)
	if err != nil {
		return Repository{}, err
	}
	sha, err := publicRepositoryHead(ctx, owner, repo)
	if err != nil {
		return Repository{}, fmt.Errorf("read repository details: %w", err)
	}
	return Repository{Owner: owner, Repo: repo, SHA: sha, Modules: []string{}}, nil
}

func publicRepositoryHead(ctx context.Context, owner, repo string) (string, error) {
	address := "https://github.com/" + owner + "/" + repo + ".git"
	command := exec.CommandContext(ctx, "git", "ls-remote", address, "HEAD")
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=/nonexistent",
		"XDG_CONFIG_HOME=/nonexistent",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
	}
	for _, key := range []string{"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value, exists := os.LookupEnv(key); exists {
			command.Env = append(command.Env, key+"="+value)
		}
	}
	output, err := command.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("public GitHub repository not found or unavailable; private repositories are not supported")
	}
	for _, line := range strings.Split(string(output), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[1] == "HEAD" && shaPattern.MatchString(parts[0]) {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("public GitHub repository has no default branch commit")
}
