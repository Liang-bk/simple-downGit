package main

// 主要对github路径做一些处理
import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

type FileType int

const (
	NULL FileType = iota
	DIR
	FILE
)

// githubURLInfo 存储github url中对应的信息
type githubURLInfo struct {
	owner    string
	repo     string
	path     string
	pathType string // "tree" 是目录, "blob" 是file.
}
type GithubEntry struct {
	Name string
	Type FileType
	URL  string
}

// apiResponseItem defines the structure for unmarshalling the GitHub API response.
// It now includes a nested struct to handle the "_links" object.
type apiResponseItem struct {
	Name    string `json:"name"`
	Type    string `json:"type"` // GitHub API uses "file" and "dir".
	HtmlURL string `json:"html_url"`
}

// listGithubDirContents fetches the contents of a directory from a GitHub repository.
// It requires a URL pointing to a directory (containing "tree").
func listGithubDirContents(url string) ([]GithubEntry, error) {
	// 1. First, validate that the URL is a directory.
	pathType, err := checkGithubPathType(url)
	if err != nil {
		return nil, fmt.Errorf("URL validation failed: %w", err)
	}
	if pathType != DIR {
		return nil, errors.New("the provided URL is not a directory (must contain '/tree/')")
	}

	// 2. Parse the URL to get its components.
	info, err := parseGithubURL(url)
	if err != nil {
		return nil, fmt.Errorf("could not parse URL: %w", err)
	}
	// 3. Construct the GitHub API URL.
	const githubAPI = "https://api.github.com/repos/"
	apiURL := fmt.Sprintf("%s%s/%s/contents/%s", githubAPI, info.owner, info.repo, info.path)

	// 4. Make the HTTP GET request to the GitHub API.
	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned a non-200 status: %s", resp.Status)
	}

	// 5. Read and parse the JSON response.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var apiResponse []apiResponseItem
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	// 6. Convert the API response to the user-facing GithubEntry slice.
	var entries []GithubEntry
	for _, item := range apiResponse {
		entryType := FILE
		if item.Type == "dir" {
			entryType = DIR
		}
		entries = append(entries, GithubEntry{
			Name: item.Name,
			Type: entryType,
			URL:  item.HtmlURL,
		})
	}

	return entries, nil
}

// checkGithubPathType determines if a GitHub URL points to a file or a directory.
// It accepts a URL and returns "directory", "file", or an error.
func checkGithubPathType(httpURL string) (FileType, error) {
	info, err := parseGithubURL(httpURL)
	if err != nil {
		return NULL, err
	}

	switch info.pathType {
	case "tree":
		return DIR, nil
	case "blob":
		return FILE, nil
	default:
		// This case should not be reached if the regex is correct.
		return NULL, errors.New("unknown path type")
	}
}

// parseGithubURL parses a GitHub URL to extract owner, repo, path, and type (tree/blob).
func parseGithubURL(httpURL string) (*githubURLInfo, error) {
	// Updated regex to capture the type ('tree' or 'blob') as the 3rd group.
	re := regexp.MustCompile(`https?://github\.com/([^/]+)/([^/]+)/(tree|blob)/[^/]+/(.+)`)
	matches := re.FindStringSubmatch(httpURL)

	// We expect 5 matches: full string, owner, repo, type, and path.
	if len(matches) < 5 {
		return nil, errors.New("invalid or unsupported Github URL format")
	}

	return &githubURLInfo{
		owner:    matches[1],
		repo:     matches[2],
		pathType: matches[3], // 'tree' or 'blob'
		path:     matches[4],
	}, nil
}
