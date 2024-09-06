package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/exp/maps"
	"oras.land/oras-go/pkg/registry/remote"
)

const (
	repo    = "kev_fan/kuadrant-operator"
	baseURL = "https://quay.io/api/v1/repository/"
)

var (
	accessToken       = os.Getenv("ACCESS_TOKEN")
	preserveSubstring = "latest" // Example Tag name that wont be deleted i.e relevant tags
)

// Tag represents a tag in the repository.
type Tag struct {
	Name         string `json:"name"`
	LastModified string `json:"last_modified"`
}

// TagsResponse represents the structure of the API response that contains tags.
type TagsResponse struct {
	Tags []Tag `json:"tags"`
}

func main() {
	client := &http.Client{}

	logger := log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime)

	if accessToken == "" {
		logger.Fatalln("no access token provided")
	}

	// Fetch tags from the API
	tags, err := fetchTags(client)
	if err != nil {
		logger.Fatalln("Error fetching tags:", err)
	}

	// Use filterTags to get tags to delete and remaining tags
	tagsToDelete, remainingTags, err := filterTags(tags, preserveSubstring)
	if err != nil {
		logger.Fatalln("Error filtering tags:", err)
	}

	// Delete tags and update remainingTags
	for tagName := range tagsToDelete {
		if err := deleteTag(client, accessToken, tagName); err != nil {
			logger.Println("Error deleting tag:", err)
			continue
		}

		logger.Printf("Successfully deleted tag: %s\n", tagName)

		delete(remainingTags, tagName) // Remove deleted tag from remainingTags
	}

	// Print remaining tags
	logger.Println("Remaining tags:", maps.Keys(remainingTags))
}

// fetchTags retrieves the tags from the repository using the Quay.io API.
func fetchTags(client remote.Client) ([]Tag, error) {
	req, err := http.NewRequest("GET", baseURL+repo+"/tag", nil)
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

	return tagsResp.Tags, nil
}

// filterTags takes a slice of tags and returns two maps: one for tags to delete and one for remaining tags.
func filterTags(tags []Tag, preserveSubstring string) (map[string]struct{}, map[string]struct{}, error) {
	// Calculate the cutoff time
	cutOffTime := time.Now().AddDate(0, 0, 0).Add(0 * time.Hour).Add(-1 * time.Minute)

	tagsToDelete := make(map[string]struct{})
	remainingTags := make(map[string]struct{})

	for _, tag := range tags {
		// Parse the LastModified timestamp
		lastModified, err := time.Parse(time.RFC1123, tag.LastModified)
		if err != nil {
			return nil, nil, err
		}

		// Check if tag should be deleted
		if lastModified.Before(cutOffTime) && !containsSubstring(tag.Name, preserveSubstring) {
			tagsToDelete[tag.Name] = struct{}{}
		} else {
			remainingTags[tag.Name] = struct{}{}
		}
	}

	return tagsToDelete, remainingTags, nil
}

func containsSubstring(tagName, substring string) bool {
	return strings.Contains(tagName, substring)
}

// deleteTag sends a DELETE request to remove the specified tag from the repository
// Returns true if successful, false otherwise
func deleteTag(client remote.Client, accessToken, tagName string) error {
	req, err := http.NewRequest("DELETE", baseURL+repo+"/tag/"+tagName, nil)
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
