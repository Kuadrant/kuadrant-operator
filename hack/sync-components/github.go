// Lightweight GitHub API client using net/http. We only need a handful of
// endpoints (repo info, file contents, commit resolution) so a full client
// library like google/go-github isn't justified. Revisit if the tool grows
// to need more GitHub API surface.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var httpClient = &http.Client{
	Timeout: 60 * time.Second,
}

func newGitHubRequest(method, url string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	if token := os.Getenv("GH_TOKEN"); token != "" {
		req.Header.Set("Authorization", "token "+token)
	} else if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	return req, nil
}

func githubGet(url string, target any) error {
	req, err := newGitHubRequest("GET", url)
	if err != nil {
		return err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &ghAPIError{StatusCode: resp.StatusCode, URL: url}
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

func githubGetRaw(url string) (io.ReadCloser, error) {
	req, err := newGitHubRequest("GET", url)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s: %w", url, err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, &ghAPIError{StatusCode: resp.StatusCode, URL: url}
	}

	return resp.Body, nil
}

func getDefaultBranch(repo string) (string, error) {
	var repoInfo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := githubGet(fmt.Sprintf("https://api.github.com/repos/%s", repo), &repoInfo); err != nil {
		return "", err
	}
	return repoInfo.DefaultBranch, nil
}

type ghAPIError struct {
	StatusCode int
	URL        string
}

func (e *ghAPIError) Error() string {
	return fmt.Sprintf("HTTP %d from %s", e.StatusCode, e.URL)
}

func getFileContent(repo, path, ref string) ([]byte, error) {
	reqURL := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s?ref=%s", repo, path, url.QueryEscape(ref))

	req, err := newGitHubRequest("GET", reqURL)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s: %w", reqURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &ghAPIError{StatusCode: resp.StatusCode, URL: reqURL}
	}

	var file struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&file); err != nil {
		return nil, fmt.Errorf("decoding response from %s: %w", reqURL, err)
	}
	if file.Encoding != "base64" {
		return nil, fmt.Errorf("unexpected encoding %q from %s", file.Encoding, reqURL)
	}
	cleaned := strings.ReplaceAll(file.Content, "\n", "")
	return base64.StdEncoding.DecodeString(cleaned)
}

func resolveCommitSHA(repo, ref string) (string, error) {
	var commit struct {
		SHA string `json:"sha"`
	}
	reqURL := fmt.Sprintf("https://api.github.com/repos/%s/commits/%s", repo, url.PathEscape(ref))
	if err := githubGet(reqURL, &commit); err != nil {
		return "", err
	}
	return commit.SHA, nil
}
