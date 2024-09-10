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

	"golang.org/x/exp/maps"
	"oras.land/oras-go/pkg/registry/remote"
)

const (
	// Max number of entries returned as specified in Quay API docs for listing tags
	pageLimit = 100
)

var (
	accessToken        = os.Getenv("ACCESS_TOKEN")
	preserveSubstrings = []string{
		"latest",
		// Preserve semver release branch or semver tag regex - release-vX.Y.Z(-rc1) or vX.Y.Z(-rc1)
		// Based on https://semver.org/#is-there-a-suggested-regular-expression-regex-to-check-a-semver-string
		"^(v|release-v)(?P<major>0|[1-9]\\d*)\\.(?P<minor>0|[1-9]\\d*)\\.(?P<patch>0|[1-9]\\d*)(?:-(?P<prerelease>(?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\\.(?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\\+(?P<buildmetadata>[0-9a-zA-Z-]+(?:\\.[0-9a-zA-Z-]+)*))?$",
	}
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
	baseURL := flag.String("base-url", "https://quay.io/api/v1/repository/", "Base API URL")
	dryRun := flag.Bool("dry-run", true, "Dry run")
	flag.Parse()

	client := &http.Client{}

	logger := log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime)

	if accessToken == "" {
		logger.Fatalln("no access token provided")
	}

	// Fetch tags from the API
	tags, err := fetchTags(client, baseURL, repo)
	if err != nil {
		logger.Fatalln("Error fetching tags:", err)
	}

	// Use filterTags to get tags to delete and remaining tags
	tagsToDelete, preservedTags, err := filterTags(tags, preserveSubstrings)
	if err != nil {
		logger.Fatalln("Error filtering tags:", err)
	}

	logger.Println("Tags to delete:", maps.Keys(tagsToDelete))

	// Delete tags and update remainingTags
	for tagName := range tagsToDelete {
		if dryRun != nil && *dryRun {
			logger.Printf("DRY RUN - Successfully deleted tag: %s\n", tagName)
		} else {
			if err := deleteTag(client, baseURL, repo, accessToken, tagName); err != nil {
				logger.Println(err)
				continue
			}

			logger.Printf("Successfully deleted tag: %s\n", tagName)
		}

		delete(tagsToDelete, tagName) // Remove deleted tag from remainingTags
	}

	// Print remaining tags
	logger.Println("Preserved tags:", maps.Keys(preservedTags))
	logger.Println("Tags not deleted successfully:", maps.Keys(tagsToDelete))
}

// fetchTags retrieves the tags from the repository using the Quay.io API.
// https://docs.quay.io/api/swagger/#!/tag/listRepoTags
func fetchTags(client remote.Client, baseURL, repo *string) ([]Tag, error) {
	allTags := make([]Tag, 0)

	i := 1

	for {
		url := fmt.Sprintf("%s%s/tag/?page=%d&limit=%d", *baseURL, *repo, i, pageLimit)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("error creating request: %w", err)
		}

		// Required for private repos
		req.Header.Add("Authorization", "Bearer "+accessToken)

		// Execute the request
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("error making request: %w", err)
		}
		defer resp.Body.Close()

		// Handle possible non-200 status codes
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
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
			i += 1
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
func deleteTag(client remote.Client, baseURL, repo *string, accessToken, tagName string) error {
	req, err := http.NewRequest("DELETE", fmt.Sprintf("%s%s/tag/%s", *baseURL, *repo, tagName), nil)
	if err != nil {
		return fmt.Errorf("error creating DELETE request: %s", err)
	}
	req.Header.Add("Authorization", "Bearer "+accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error deleting tag: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("Failed to delete tag %s: Status code %d\nBody: %s\n", tagName, resp.StatusCode, string(body))
}

// filterTags takes a slice of tags and preserves string regex and returns two maps: one for tags to delete and one for remaining tags.
func filterTags(tags []Tag, preserveSubstrings []string) (map[string]struct{}, map[string]struct{}, error) {
	tagsToDelete := make(map[string]struct{})
	preservedTags := make(map[string]struct{})

	// Compile the regexes for each preserve substring
	var preserveRegexes []*regexp.Regexp
	for _, substr := range preserveSubstrings {
		regex, err := regexp.Compile(substr)
		if err != nil {
			return nil, nil, err
		}
		preserveRegexes = append(preserveRegexes, regex)
	}

	for _, tag := range tags {
		// Tags that have an expiration set are ignored as they could be historical tags that have already expired
		// i.e. when an existing tag is updated, the previous tag of the same name is expired and is returned when listing
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
