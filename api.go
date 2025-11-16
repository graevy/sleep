package main

import (
	"strings"
	"time"
	"fmt"
	"log"
	"os"
	"io"
	"encoding/json"
	"net/http"
)

type fetchFunc func(host, user string) ([]string, error)

func detectAPI(host string) fetchFunc {
	host = strings.ToLower(host)

	// try to match against a known host first
	switch {
	case strings.HasSuffix(host, "github.com"):
		return fetchGitHubRepoURLs

	case strings.HasSuffix(host, "gitlab.com"):
		return fetchGitLabRepoURLs

	case strings.HasSuffix(host, "gitea.com"),
		strings.HasSuffix(host, "codeberg.org"),
		strings.HasSuffix(host, "forgejo.org"):
		return fetchGiteaRepoURLs

	// maybe later
	// case strings.HasSuffix(host, "pagure.io"),
	// 	strings.HasSuffix(host, "fedoraproject.org"),
	// 	strings.HasSuffix(host, "freedesktop.org"):
	// 	return fetchPagureRepoURLs
	//
	// case strings.HasSuffix(host, "sourcehut.org"),
	// 	strings.HasSuffix(host, "git.sr.ht"):
	// 	return fetchSourceHutRepoURLs
	}

	client := &http.Client{
		Timeout: 3 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// fallback effort: manually probe URL for api using hacky string matching
	check := func(path string) bool {
		url := fmt.Sprintf("https://%s%s", host, path)
		resp, err := client.Get(url)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK ||
			resp.StatusCode == http.StatusUnauthorized ||
			resp.StatusCode == http.StatusForbidden
	}

	switch {
		case check("/api/v3"):
			return fetchGitHubRepoURLs
		case check("/api/v4/version"):
			return fetchGitLabRepoURLs
		case check("/api/v1/version"):
			return fetchGiteaRepoURLs
		default:
			return nil
	}
}

func fetchGitHubRepoURLs(host string, username string) ([]string, error) {
	log.Printf("matched host %s to github API, attempting to fetch repos...", host)

	// apiURL := fmt.Sprintf("https://api.github.com/users/%s/repos?per_page=100", username)
	apiURL := fmt.Sprintf("https://api.github.com/users/%s/repos?type=public&sort=pushed&direction=desc&per_page=100", username)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "go-commit-plotter")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API request failed: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var repos []struct {
		CloneURL string `json:"clone_url"`
	}

	if err := json.Unmarshal(body, &repos); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %v", err)
	}

	var urls []string
	for _, repo := range repos {
		urls = append(urls, repo.CloneURL)
	}
	return urls, nil
}

func fetchGitLabRepoURLs(host, username string) ([]string, error) {
	log.Printf("matched host %s to gitlab API, attempting to fetch repos...", host)

	var apiBase string
	switch {
	case strings.Contains(host, "gitlab"):
		apiBase = fmt.Sprintf("https://%s/api/v4/users/%s/projects", host, username)
	case strings.Contains(host, "gitea"):
		apiBase = fmt.Sprintf("https://%s/api/v1/users/%s/repos", host, username)
	default:
		return nil, fmt.Errorf("unsupported GitLab/Gitea host: %s", host)
	}

	apiURL := apiBase + "?order_by=last_activity_at&sort=desc&per_page=100&per_page=100"

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "go-commit-plotter")

	if token := os.Getenv("GITLAB_TOKEN"); token != "" && strings.Contains(host, "gitlab") {
		req.Header.Set("PRIVATE-TOKEN", token)
	}
	if token := os.Getenv("GITEA_TOKEN"); token != "" && strings.Contains(host, "gitea") {
		req.Header.Set("Authorization", "token "+token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed (%s): %s", resp.Status, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var repos []map[string]any
	if err := json.Unmarshal(body, &repos); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	var urls []string
	for _, repo := range repos {
		switch {
		case repo["http_url_to_repo"] != nil:
			urls = append(urls, repo["http_url_to_repo"].(string))
		case repo["clone_url"] != nil:
			urls = append(urls, repo["clone_url"].(string))
		case repo["ssh_url_to_repo"] != nil:
			urls = append(urls, repo["ssh_url_to_repo"].(string))
		}
	}
	return urls, nil
}

func fetchGiteaRepoURLs(host, username string) ([]string, error) {
	log.Printf("matched host %s to gitea API, attempting to fetch repos...", host)

	apiURL := fmt.Sprintf("https://%s/api/v1/users/%s/repos?sort=updated&limit=100", host, username)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "go-commit-plotter")
	if token := os.Getenv("GITEA_TOKEN"); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitea API request failed: %s, %s", resp.Status, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var repos []struct {
		CloneURL string `json:"clone_url"`
		SSHURL   string `json:"ssh_url"`
		FullName string `json:"full_name"`
	}
	if err := json.Unmarshal(body, &repos); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	var urls []string
	for _, r := range repos {
		if r.CloneURL != "" {
			urls = append(urls, r.CloneURL)
		} else if r.SSHURL != "" {
			urls = append(urls, r.SSHURL)
		} else if r.FullName != "" {
			urls = append(urls, fmt.Sprintf("https://%s/%s.git", host, r.FullName))
		}
	}
	return urls, nil
}

