package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sync"
	"time"

	"golang.org/x/exp/maps"
)

const (
	// Max number of entries returned as specified in Quay API docs for listing tags
	pageLimit         = 100
	accessTokenEnvKey = "ACCESS_TOKEN"
)

var (
	accessToken        = os.Getenv(accessTokenEnvKey)
	preserveSubstrings = []string{
		"latest",
		// Preserve semver release branch or semver tag regex - release-vX.Y.Z(-rc1) or vX.Y.Z(-rc1)
		// Based on https://semver.org/#is-there-a-suggested-regular-expression-regex-to-check-a-semver-string
		"^(v|release-v)(?P<major>0|[1-9]\\d*)\\.(?P<minor>0|[1-9]\\d*)\\.(?P<patch>0|[1-9]\\d*)(?:-(?P<prerelease>(?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\\.(?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\\+(?P<buildmetadata>[0-9a-zA-Z-]+(?:\\.[0-9a-zA-Z-]+)*))?$",
	}
	client = &http.Client{Timeout: 5 * time.Second}
)

// Tag represents a tag in the repository.
type Tag struct {
	Name       string `json:"name"`
	Expiration string `json:"expiration"`
}

// TagsResponse represents the structure of the API response that contains tags.
type TagsResponse struct {
	Tags []Tag `json:"tags"`
	// HasAdditional denotes whether there is still additional tags to be listed in the paginated response
	HasAdditional bool `json:"has_additional"`
}

func main() {
	repo := flag.String("repo", "kuadrant/kuadrant-operator", "Repository name")
	baseURL := flag.String("base-url", "https://quay.io/api/v1/repository", "Base API URL")
	dryRun := flag.Bool("dry-run", true, "Dry run")
	batchSize := flag.Int("batch-size", 50, "Batch size for deletion. API calls might get rate limited at large values")
	flag.Parse()

	logger := log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime)

	if accessToken == "" {
		logger.Fatalln("no access token provided")
	}

	beginningTime := time.Now()

	// Fetch tags from the API
	logger.Println("Fetching tags from Quay")
	tags, err := fetchTags(baseURL, repo, accessToken)
	if err != nil {
		logger.Fatalln("Error fetching tags:", err)
	}

	// Use filterTags to get tags to delete and preserved tags
	logger.Println("Filtering tags")
	tagsToDelete, preservedTags, err := filterTags(tags, preserveSubstrings)
	if err != nil {
		logger.Fatalln("Error filtering tags:", err)
	}

	logger.Println("Tags to delete:", maps.Keys(tagsToDelete), "num", len(tagsToDelete))

	var wg sync.WaitGroup

	// Delete tags in batches
	i := 0
	for tagName := range tagsToDelete {
		if i%*batchSize == 0 && i != 0 {
			// Wait for the current batch to complete before starting a new one
			wg.Wait()
		}

		wg.Add(1)
		go func(tagName string) {
			defer wg.Done()

			if *dryRun {
				logger.Printf("DRY RUN - Successfully deleted tag: %s\n", tagName)
			} else {
				if err := deleteTag(baseURL, repo, accessToken, tagName); err != nil {
					logger.Println(err)
				}

				logger.Printf("Successfully deleted tag: %s\n", tagName)
			}
		}(tagName)

		delete(tagsToDelete, tagName) // Remove deleted tag from tagsToDelete
		i++
	}

	// Wait for the final batch to complete
	wg.Wait()

	logger.Println("Preserved tags:", maps.Keys(preservedTags), "num", len(preservedTags))
	logger.Println("Tags not deleted successfully:", maps.Keys(tagsToDelete), len(tagsToDelete))
	logger.Println("Execution time:", time.Since(beginningTime).String())
}

// fetchTags retrieves the tags from the repository using the Quay.io API.
// https://docs.quay.io/api/swagger/#!/tag/listRepoTags
func fetchTags(baseURL, repo *string, accessToken string) ([]Tag, error) {
	if baseURL == nil || repo == nil {
		return nil, fmt.Errorf("baseURL or repo required")
	}

	allTags := make([]Tag, 0)
	i := 1

	for {
		url := fmt.Sprintf("%s/%s/tag/?page=%d&limit=%d", *baseURL, *repo, i, pageLimit)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("error creating request: %w", err)
		}

		// Required for private repos
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", accessToken))

		// Execute the request
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("error making request: %w", err)
		}
		defer resp.Body.Close()

		// Handle possible non-200 status codes
		if resp.StatusCode != http.StatusOK {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("error reading response body: %w", err)
			}
			return nil, fmt.Errorf("error: received status code %d\nBody: %s", resp.StatusCode, string(body))
		}

		// Read the response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading response body: %w", err)
		}

		// Parse the JSON response
		var tagsResp TagsResponse
		if err := json.Unmarshal(body, &tagsResp); err != nil {
			return nil, fmt.Errorf("error unmarshalling response: %w", err)
		}

		allTags = append(allTags, tagsResp.Tags...)

		if tagsResp.HasAdditional {
			i++
			continue
		}

		// Has no additional pages
		break
	}

	return allTags, nil
}

// deleteTag sends a DELETE request to remove the specified tag from the repository
// Returns nil if successful, error otherwise
// https://docs.quay.io/api/swagger/#!/tag/deleteFullTag
func deleteTag(baseURL, repo *string, accessToken, tagName string) error {
	if baseURL == nil || repo == nil {
		return fmt.Errorf("baseURL or repo required")
	}

	url := fmt.Sprintf("%s/%s/tag/%s", *baseURL, *repo, tagName)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("error creating DELETE request: %w", err)
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error deleting tag: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %w", err)
	}
	return fmt.Errorf("failed to delete tag %s: Status code %d Body: %s", tagName, resp.StatusCode, string(body))
}

// filterTags takes a slice of tags and preserves string regex and returns two maps: one for tags to delete and one for preserved tags.
func filterTags(tags []Tag, preserveSubstrings []string) (map[string]struct{}, map[string]struct{}, error) {
	tagsToDelete := make(map[string]struct{})
	preservedTags := make(map[string]struct{})

	// Compile the regexes for each preserve substring
	preserveRegexes := make([]*regexp.Regexp, 0, len(preserveSubstrings))
	for _, substr := range preserveSubstrings {
		regex, err := regexp.Compile(substr)
		if err != nil {
			return nil, nil, err
		}
		preserveRegexes = append(preserveRegexes, regex)
	}

	for _, tag := range tags {
		// Tags that have an expiration set are ignored as they could be historical tags that have already expired
		// i.e. when an existing tag is updated, the previous tag of the same name is expired and is still returned when listing
		// the tags
		if tag.Expiration != "" {
			continue
		}

		// Check if the tag name matches any of the preserve substrings
		preserve := false
		for _, regex := range preserveRegexes {
			if regex.MatchString(tag.Name) {
				preserve = true
				break
			}
		}

		if !preserve {
			tagsToDelete[tag.Name] = struct{}{}
		} else {
			preservedTags[tag.Name] = struct{}{}
		}
	}

	return tagsToDelete, preservedTags, nil
}
