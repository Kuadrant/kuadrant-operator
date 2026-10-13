//go:build integration && mcp_auth_e2e

package istio_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	. "github.com/onsi/gomega"
)

func TestMCPAuthCallTool(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
		wantError   bool
	}{
		{
			name:        "successful result without isError",
			contentType: "application/json",
			body:        `{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"Hi e2e"}]}}`,
		},
		{
			name:        "JSON tool execution error",
			contentType: "application/json",
			body:        `{"jsonrpc":"2.0","id":%d,"result":{"isError":true,"content":[{"type":"text","text":"upstream tool failed"}]}}`,
			wantError:   true,
		},
		{
			name:        "SSE tool execution error",
			contentType: "text/event-stream",
			body:        "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"isError\":true,\"content\":[{\"type\":\"text\",\"text\":\"upstream tool failed\"}]}}\n\n",
			wantError:   true,
		},
	}
	for i := range cases {
		testCase := &cases[i]
		t.Run(testCase.name, func(t *testing.T) {
			g := NewWithT(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					ID int64 `json:"id"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode tool request: %v", err)
					return
				}
				w.Header().Set("Content-Type", testCase.contentType)
				if _, err := fmt.Fprintf(w, testCase.body, request.ID); err != nil {
					t.Errorf("write tool response: %v", err)
				}
			}))
			t.Cleanup(server.Close)

			status, content, err := mcpAuthCallTool(t.Context(), server.URL, "", "test1_greet", map[string]any{"name": "e2e"}, nil)
			if testCase.wantError {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(status).To(Equal(http.StatusOK))
			g.Expect(content).To(ContainElement(HaveKeyWithValue("text", "Hi e2e")))
		})
	}
}

func TestMCPAuthCallToolSSECompletesBeforeEOF(t *testing.T) {
	cases := []struct {
		name      string
		unrelated string
		body      string
		wantError bool
	}{
		{
			name:      "matching result after notification and unrelated error",
			unrelated: `"error":{"code":-32603,"message":"unrelated request failed"}`,
			body:      `"result":{"content":[{"type":"text","text":"Hi e2e"}]}`,
		},
		{
			name:      "matching error after notification and unrelated result",
			unrelated: `"result":{"content":[{"type":"text","text":"stale result"}]}`,
			body:      `"error":{"code":-32603,"message":"requested tool failed"}`,
			wantError: true,
		},
	}
	for i := range cases {
		testCase := &cases[i]
		t.Run(testCase.name, func(t *testing.T) {
			g := NewWithT(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					ID int64 `json:"id"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode tool request: %v", err)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if _, err := fmt.Fprintf(w,
					"event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progress\":1}}\n\n"+
						"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,%s}\n\n"+
						"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\ndata: %s}\n\n",
					request.ID+1, testCase.unrelated, request.ID, testCase.body); err != nil {
					t.Errorf("write streamed tool response: %v", err)
					return
				}
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			t.Cleanup(server.Close)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			status, content, err := mcpAuthCallTool(ctx, server.URL, "", "test1_greet", map[string]any{"name": "e2e"}, nil)
			g.Expect(ctx.Err()).NotTo(HaveOccurred())
			g.Expect(status).To(Equal(http.StatusOK))
			if testCase.wantError {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(content).To(ContainElement(HaveKeyWithValue("text", "Hi e2e")))
		})
	}
}

func TestMCPAuthGetPrompt(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantText  []string
		wantError bool
	}{
		{
			name: "text messages from successful result",
			body: `{"jsonrpc":"2.0","id":%d,"result":{"messages":[` +
				`{"role":"user","content":{"type":"text","text":"Say hi to reviewer"}},` +
				`{"role":"user","content":{"type":"image","mimeType":"image/png","data":"aW1hZ2U="}},` +
				`{"role":"assistant","content":{"type":"text","text":"A second instruction"}}]}}`,
			wantText: []string{"Say hi to reviewer", "A second instruction"},
		},
		{
			name:      "JSON-RPC error mentioning expected phrase",
			body:      `{"jsonrpc":"2.0","id":%d,"error":{"code":-32603,"message":"Unable to produce Say hi to reviewer"}}`,
			wantError: true,
		},
		{
			name: "expected phrase outside text messages",
			body: `{"jsonrpc":"2.0","id":%d,"result":{"description":"Say hi to reviewer","messages":[` +
				`{"role":"user","content":{"type":"resource","resource":{"uri":"test://prompt","text":"Say hi to reviewer"}}},` +
				`{"role":"user","content":{"type":"text","text":"A different instruction"}}]}}`,
			wantText: []string{"A different instruction"},
		},
	}
	for _, contentType := range []string{"application/json", "text/event-stream"} {
		t.Run(contentType, func(t *testing.T) {
			for i := range cases {
				testCase := &cases[i]
				t.Run(testCase.name, func(t *testing.T) {
					g := NewWithT(t)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						var request struct {
							ID int64 `json:"id"`
						}
						if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
							t.Errorf("decode prompt request: %v", err)
							return
						}
						w.Header().Set("Content-Type", contentType)
						body := testCase.body
						if contentType == "text/event-stream" {
							body = "event: message\ndata: " + body + "\n\n"
						}
						if _, err := fmt.Fprintf(w, body, request.ID); err != nil {
							t.Errorf("write prompt response: %v", err)
						}
					}))
					t.Cleanup(server.Close)

					status, text, err := mcpAuthGetPrompt(t.Context(), server.URL, "", "test1_greet", map[string]string{"name": "reviewer"}, nil)
					g.Expect(status).To(Equal(http.StatusOK))
					if testCase.wantError {
						g.Expect(err).To(HaveOccurred())
						g.Expect(text).To(BeEmpty())
						return
					}
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(text).To(Equal(testCase.wantText))
				})
			}
		})
	}
}
