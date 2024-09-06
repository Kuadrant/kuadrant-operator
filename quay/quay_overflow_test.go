package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

type MockHTTPClient struct {
	wantErr  bool
	mutateFn func(res *http.Response)
}

func (m MockHTTPClient) Do(_ *http.Request) (*http.Response, error) {
	if m.wantErr {
		return nil, errors.New("oops")
	}

	resp := &http.Response{}
	if m.mutateFn != nil {
		m.mutateFn(resp)
	}

	return resp, nil
}

var _ rest.HTTPClient = &MockHTTPClient{}

func Test_fetchTags(t *testing.T) {
	t.Run("test error making request", func(t *testing.T) {
		tags, err := fetchTags(&MockHTTPClient{wantErr: true})

		if err == nil {
			t.Error("error expected")
		}

		if err.Error() != "error making request: oops" {
			t.Errorf("error expected, got %s", err.Error())
		}

		if tags != nil {
			t.Error("expected nil tags")
		}
	})

	t.Run("test error for non-200 status codes", func(t *testing.T) {
		tags, err := fetchTags(&MockHTTPClient{mutateFn: func(res *http.Response) {
			res.Status = string(rune(400))
			res.Body = io.NopCloser(bytes.NewReader(nil))
		}})

		if err == nil {
			t.Error("error expected")
		}

		if strings.Contains(err.Error(), "tags, error: received status code 400") {
			t.Errorf("error expected, got %s", err.Error())
		}

		if tags != nil {
			t.Error("expected nil tags")
		}
	})

	t.Run("test error parsing json", func(t *testing.T) {
		tags, err := fetchTags(&MockHTTPClient{mutateFn: func(res *http.Response) {
			res.Status = string(rune(200))
			res.Body = io.NopCloser(bytes.NewReader([]byte("{notTags: error}")))
		}})

		if err == nil {
			t.Error("error expected")
		}

		if strings.Contains(err.Error(), "error unmarshalling response:") {
			t.Errorf("error expected, got %s", err.Error())
		}

		if tags != nil {
			t.Error("expected nil tags")
		}
	})

	t.Run("test successful response with tags", func(t *testing.T) {
		mockJSONResponse := `{
			"tags": [
				{"name": "v1.0.0", "last_modified": "Mon, 02 Jan 2006 15:04:05 MST"},
				{"name": "v1.1.0", "last_modified": "Tue, 03 Jan 2006 15:04:05 MST"},
				{"name": "latest", "last_modified": "Wed, 04 Jan 2006 15:04:05 MST"}
			]
		}`

		tags, err := fetchTags(&MockHTTPClient{mutateFn: func(res *http.Response) {
			res.StatusCode = http.StatusOK
			res.Body = io.NopCloser(bytes.NewReader([]byte(mockJSONResponse)))
		}})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Validate the returned tags
		if len(tags) != 3 {
			t.Fatalf("expected 3 tags, got %d", len(tags))
		}

		expectedTags := map[string]string{
			"v1.0.0": "Mon, 02 Jan 2006 15:04:05 MST",
			"v1.1.0": "Tue, 03 Jan 2006 15:04:05 MST",
			"latest": "Wed, 04 Jan 2006 15:04:05 MST",
		}

		for _, tag := range tags {
			if expectedDate, ok := expectedTags[tag.Name]; !ok || expectedDate != tag.LastModified {
				t.Errorf("unexpected tag: got %v, expected %v", tag, expectedTags[tag.Name])
			}
		}
	})
}

func Test_deleteTag(t *testing.T) {
	t.Run("test successful delete", func(t *testing.T) {
		client := &MockHTTPClient{mutateFn: func(res *http.Response) {
			res.StatusCode = http.StatusNoContent
			res.Body = io.NopCloser(bytes.NewReader(nil))
		}}

		err := deleteTag(client, "fake_access_token", "v1.0.0")

		if err != nil {
			t.Error("expected successful delete, got error")
		}
	})

	t.Run("test delete with error response", func(t *testing.T) {
		client := &MockHTTPClient{mutateFn: func(res *http.Response) {
			res.StatusCode = http.StatusInternalServerError
			res.Body = io.NopCloser(bytes.NewReader([]byte("internal server error")))
		}}

		err := deleteTag(client, "fake_access_token", "v1.0.0")

		if err == nil {
			t.Error("expected failure, got success")
		}
	})

	t.Run("test error making delete request", func(t *testing.T) {
		client := &MockHTTPClient{wantErr: true}

		err := deleteTag(client, "fake_access_token", "v1.0.0")

		if err == nil {
			t.Error("expected failure, got success")
		}
	})
}

func Test_filterTags(t *testing.T) {
	t.Run("test filter tags correctly", func(t *testing.T) {
		tags := []Tag{
			{"nightly-build", time.Now().Add(-24 * time.Hour).Format(time.RFC1123)}, // Old tag, should be deleted
			{"v1.1.0", time.Now().Format(time.RFC1123)},                             // Recent tag, should be kept
			{"latest", time.Now().Add(-24 * time.Hour).Format(time.RFC1123)},        // Old tag, but name contains preserveSubstring
		}

		tagsToDelete, remainingTags, err := filterTags(tags, preserveSubstring)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if len(tagsToDelete) != 1 || len(remainingTags) != 2 {
			t.Fatalf("expected 1 tag to delete and 2 remaining, got %d to delete and %d remaining", len(tagsToDelete), len(remainingTags))
		}

		if _, ok := tagsToDelete["nightly-build"]; !ok {
			t.Error("expected nightly-build to be deleted")
		}

		if _, ok := remainingTags["v1.1.0"]; !ok {
			t.Error("expected v1.1.0 to be kept")
		}

		if _, ok := remainingTags["latest"]; !ok {
			t.Error("expected latest to be kept")
		}
	})

	t.Run("test filter tags with no deletions", func(t *testing.T) {
		tags := []Tag{
			{"v1.1.0", time.Now().Format(time.RFC1123)}, // Recent tag, should be kept
			{"latest", time.Now().Format(time.RFC1123)}, // Recent tag, should be kept
		}

		tagsToDelete, remainingTags, err := filterTags(tags, preserveSubstring)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if len(tagsToDelete) != 0 || len(remainingTags) != 2 {
			t.Fatalf("expected 0 tags to delete and 2 remaining, got %d to delete and %d remaining", len(tagsToDelete), len(remainingTags))
		}
	})

	t.Run("test error unexpected time format", func(t *testing.T) {
		tags := []Tag{
			{"v1.1.0", time.Now().Format(time.ANSIC)},
		}

		_, _, err := filterTags(tags, preserveSubstring)

		if err == nil {
			t.Fatal("expected error, got success")
		}
	})
}
