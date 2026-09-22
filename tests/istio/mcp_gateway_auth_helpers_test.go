//go:build integration && mcp_auth_e2e

package istio_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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
			body:        `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Hi e2e"}]}}`,
		},
		{
			name:        "JSON tool execution error",
			contentType: "application/json",
			body:        `{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[{"type":"text","text":"upstream tool failed"}]}}`,
			wantError:   true,
		},
		{
			name:        "SSE tool execution error",
			contentType: "text/event-stream",
			body:        "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"isError\":true,\"content\":[{\"type\":\"text\",\"text\":\"upstream tool failed\"}]}}\n\n",
			wantError:   true,
		},
	}
	for i := range cases {
		testCase := &cases[i]
		t.Run(testCase.name, func(t *testing.T) {
			g := NewWithT(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", testCase.contentType)
				if _, err := io.WriteString(w, testCase.body); err != nil {
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
