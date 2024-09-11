package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

var (
	testBaseURL     = "https://quay.io/api/v1/"
	testRepo        = "testOrg/kuadrant-operator"
	testAccessToken = "fake_access_token"
)

func Test_fetchTags(t *testing.T) {
	t.Run("test error for non-200 status codes", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()
		tags, err := fetchTags(&server.URL, &testRepo, testAccessToken)

		if err == nil {
			t.Error("error expected")
		}

		if !strings.Contains(err.Error(), "error: received status code 500") {
			t.Errorf("error expected, got %s", err.Error())
		}

		if tags != nil {
			t.Error("expected nil tags")
		}
	})

	t.Run("test error parsing json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("{notTags: error}"))
		}))
		defer server.Close()
		tags, err := fetchTags(&server.URL, &testRepo, testAccessToken)

		if err == nil {
			t.Error("error expected")
		}

		if !strings.Contains(err.Error(), "error unmarshalling response:") {
			t.Errorf("error expected, got %s", err.Error())
		}

		if tags != nil {
			t.Error("expected nil tags")
		}
	})

	t.Run("test bearer token is added to header", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != fmt.Sprintf("Bearer %s", testAccessToken) {
				t.Errorf("unexpected authorization header: %v", r.Header.Get("Authorization"))
			}
		}))
		defer server.Close()

		_, _ = fetchTags(&server.URL, &testRepo, testAccessToken)
	})

	t.Run("test successful response with tags", func(t *testing.T) {
		mockJSONResponse := `{
			"tags": [
				{"name": "v1.0.0"},
				{"name": "v1.1.0"},
				{"name": "latest"}
			]
		}`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(mockJSONResponse))
		}))
		defer server.Close()

		tags, err := fetchTags(&server.URL, &testRepo, testAccessToken)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Validate the returned tags
		if len(tags) != 3 {
			t.Fatalf("expected 3 tags, got %d", len(tags))
		}

		expectedTags := []string{
			"v1.0.0",
			"v1.1.0",
			"latest",
		}

		for _, tag := range tags {
			if !slices.Contains(expectedTags, tag.Name) {
				t.Errorf("unexpected tag: %v, does not exist in expected tags %v", tag, expectedTags)
			}
		}
	})

	t.Run("test error nil baseUrl", func(t *testing.T) {
		_, err := fetchTags(nil, &testRepo, testAccessToken)
		if err == nil {
			t.Fatal("error expected")
		}

		if err.Error() != "baseURL or repo required" {
			t.Errorf("error expected, got %s", err.Error())
		}
	})

	t.Run("test error nil repo", func(t *testing.T) {
		_, err := fetchTags(&testBaseURL, nil, testAccessToken)
		if err == nil {
			t.Fatal("error expected")
		}

		if err.Error() != "baseURL or repo required" {
			t.Errorf("error expected, got %s", err.Error())
		}
	})
}

func Test_deleteTag(t *testing.T) {
	t.Run("test successful delete", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		err := deleteTag(&server.URL, &testRepo, testAccessToken, "v1.0.0")

		if err != nil {
			t.Errorf("expected successful delete, got error: %s", err.Error())
		}
	})

	t.Run("test delete with error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		err := deleteTag(&server.URL, &testRepo, testAccessToken, "v1.0.0")

		if err == nil {
			t.Error("expected failure, got success")
		}

		if !strings.Contains(err.Error(), "failed to delete tag v1.0.0: Status code 500") {
			t.Errorf("error expected, got %s", err.Error())
		}
	})

	t.Run("test bearer token is added to header", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != fmt.Sprintf("Bearer %s", testAccessToken) {
				t.Errorf("unexpected authorization header: %v", r.Header.Get("Authorization"))
			}
		}))
		defer server.Close()

		_ = deleteTag(&server.URL, &testRepo, testAccessToken, "v1.0.0")
	})

	t.Run("test error nil baseUrl", func(t *testing.T) {
		err := deleteTag(nil, &testRepo, testAccessToken, "v1.0.0")

		if err == nil {
			t.Error("expected failure, got success")
		}

		if err.Error() != "baseURL or repo required" {
			t.Errorf("error expected, got %s", err.Error())
		}
	})

	t.Run("test error nil repo", func(t *testing.T) {
		err := deleteTag(&testBaseURL, nil, testAccessToken, "v1.0.0")

		if err == nil {
			t.Error("expected failure, got success")
		}

		if err.Error() != "baseURL or repo required" {
			t.Errorf("error expected, got %s", err.Error())
		}
	})
}

func Test_filterTags(t *testing.T) {
	t.Run("test filter tags correctly", func(t *testing.T) {
		tags := []Tag{
			{Name: "nightly-build"},  // Not a preserved tag, should be deleted
			{Name: "latest"},         // Preserved tag, name is latest
			{Name: "release-v1.0.0"}, // Preserved tag, name contains preserveSubstring branch release semver, release-v*
			{Name: "v1.0.0"},         // Preserved tag, but name contains preserveSubstring tag semver release
			{Name: "v1.1.0-rc1"},     // Preserved tag, but name contains preserveSubstring tag semver release-candidate
			{Name: "expiry_set", Expiration: time.Now().Format(time.RFC1123)}, // Skipped tag, already has an expiry set
			{Name: "release-not-semver"},                                      // Not a preserved tag, should be deleted
		}

		tagsToDelete, remainingTags, err := filterTags(tags, preserveSubstrings)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if len(tagsToDelete) != 2 || len(remainingTags) != 4 {
			t.Fatalf("expected 2 tag to delete and 4 remaining, got %d to delete and %d remaining", len(tagsToDelete), len(remainingTags))
		}

		if _, ok := tagsToDelete["nightly-build"]; !ok {
			t.Error("expected nightly-build to be deleted")
		}

		if _, ok := tagsToDelete["release-not-semver"]; !ok {
			t.Error("expected release-not-semver to be deleted")
		}

		if _, ok := remainingTags["latest"]; !ok {
			t.Error("expected latest to be kept")
		}

		if _, ok := remainingTags["release-v1.0.0"]; !ok {
			t.Error("expected release-v1.0.0 to be kept")
		}

		if _, ok := remainingTags["v1.0.0"]; !ok {
			t.Error("expected v1.0.0 to be kept")
		}

		if _, ok := remainingTags["v1.1.0-rc1"]; !ok {
			t.Error("expected v1.1.0-rc1 to be kept")
		}
	})

	t.Run("test filter tags with no deletions", func(t *testing.T) {
		tags := []Tag{
			{Name: "v1.1.0"}, // Preserved tag, should be kept
			{Name: "latest"}, // Preserved tag, should be kept
		}

		tagsToDelete, remainingTags, err := filterTags(tags, preserveSubstrings)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if len(tagsToDelete) != 0 || len(remainingTags) != 2 {
			t.Fatalf("expected 0 tags to delete and 2 remaining, got %d to delete and %d remaining", len(tagsToDelete), len(remainingTags))
		}
	})
}
