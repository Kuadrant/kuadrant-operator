package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"k8s.io/client-go/rest"
)

const (
	repo    = "kuadrant/kuadrant-operator"
	baseURL = "https://quay.io/api/v1/repository/"
)

var (
	robotPass         = os.Getenv("ROBOT_PASS")
	robotUser         = os.Getenv("ROBOT_USER")
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

	// Fetch tags from the API
	tags, err := fetchTags(client)
	if err != nil {
		fmt.Println("Error fetching tags:", err)
		return
	}

	// Use filterTags to get tags to delete and remaining tags
	tagsToDelete, remainingTags := filterTags(tags, preserveSubstring)

	// Delete tags and update remainingTags
	for tagName := range tagsToDelete {
		if deleteTag(client, accessToken, tagName) {
			delete(remainingTags, tagName) // Remove deleted tag from remainingTags
		}
	}

	// Print remaining tags
	fmt.Println("Remaining tags:")
	for tag := range remainingTags {
		fmt.Println(tag)
	}
}

// fetchTags retrieves the tags from the repository using the Quay.io API.
func fetchTags(client rest.HTTPClient) ([]Tag, error) {
	// TODO - DO you want to seperate out builidng the request to a function to unit test?
	// TODO - Is adding the headers even needed to fetch tags for a public repo?
	req, err := http.NewRequest("GET", baseURL+repo+"/tag", nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	// Prioritize Bearer token for authorization
	if accessToken != "" {
		req.Header.Add("Authorization", "Bearer "+accessToken)
	} else {
		// Fallback to Basic Authentication if no access token
		auth := base64.StdEncoding.EncodeToString([]byte(robotUser + ":" + robotPass))
		req.Header.Add("Authorization", "Basic "+auth)
	}

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
func filterTags(tags []Tag, preserveSubstring string) (map[string]struct{}, map[string]struct{}) {
	// Calculate the cutoff time
	cutOffTime := time.Now().AddDate(0, 0, 0).Add(0 * time.Hour).Add(-1 * time.Minute)

	tagsToDelete := make(map[string]struct{})
	remainingTags := make(map[string]struct{})

	for _, tag := range tags {
		// Parse the LastModified timestamp
		lastModified, err := time.Parse(time.RFC1123, tag.LastModified)
		if err != nil {
			fmt.Println("Error parsing time:", err)
			continue
		}

		// Check if tag should be deleted
		if lastModified.Before(cutOffTime) && !containsSubstring(tag.Name, preserveSubstring) {
			tagsToDelete[tag.Name] = struct{}{}
		} else {
			remainingTags[tag.Name] = struct{}{}
		}
	}

	return tagsToDelete, remainingTags
}

func containsSubstring(tagName, substring string) bool {
	return strings.Contains(tagName, substring)
}

// deleteTag sends a DELETE request to remove the specified tag from the repository
// Returns true if successful, false otherwise
func deleteTag(client rest.HTTPClient, accessToken, tagName string) bool {
	req, err := http.NewRequest("DELETE", baseURL+repo+"/tag/"+tagName, nil)
	if err != nil {
		fmt.Println("Error creating DELETE request:", err)
		return false
	}
	req.Header.Add("Authorization", "Bearer "+accessToken)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error deleting tag:", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		fmt.Printf("Successfully deleted tag: %s\n", tagName)
		return true
	} else {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Failed to delete tag %s: Status code %d\nBody: %s\n", tagName, resp.StatusCode, string(body))
		return false
	}
}