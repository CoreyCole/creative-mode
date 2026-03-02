package linear

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient creates a Client pointed at a test server.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c := NewClient("lin_api_test_key", "CM", slog.Default())
	c.client = srv.Client()
	c.client.Transport = rewriteTransport{url: srv.URL}

	return c
}

// rewriteTransport redirects all requests to the test server URL.
type rewriteTransport struct {
	url string
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(rt.url, "http://")

	return http.DefaultTransport.RoundTrip(req)
}

// writeJSON is a test helper that writes a JSON response and fails on error.
func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTicket(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "lin_api_test_key" {
			t.Error("expected Authorization header")
		}

		var req struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}

		if strings.Contains(req.Query, "teams(") {
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"teams": map[string]any{
						"nodes": []map[string]any{
							{"id": "team-uuid-123", "key": "CM"},
						},
					},
				},
			})

			return
		}

		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"issueCreate": map[string]any{
					"success": true,
					"issue": map[string]any{
						"id":         "issue-uuid-456",
						"identifier": "CM-42",
						"title":      "Test ticket",
						"url":        "https://linear.app/cm/issue/CM-42",
					},
				},
			},
		})
	}

	c := newTestClient(t, handler)
	id, err := c.CreateTicket(t.Context(), "Test ticket", "description", nil, "")
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if id != "CM-42" {
		t.Errorf("expected CM-42, got %s", id)
	}
}

func TestPostComment(t *testing.T) {
	t.Parallel()

	var capturedBody string

	handler := func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		capturedBody = req.Query

		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"commentCreate": map[string]any{"success": true},
			},
		})
	}

	c := newTestClient(t, handler)
	err := c.PostComment(
		t.Context(), "issue-uuid", "Phase transition: research → code_plan",
	)
	if err != nil {
		t.Fatalf("PostComment: %v", err)
	}
	if !strings.Contains(capturedBody, "commentCreate") {
		t.Error("expected commentCreate mutation")
	}
}

func TestParseIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		team    string
		number  int
		wantErr bool
	}{
		{"CRE-5", "CRE", 5, false},
		{"CM-123", "CM", 123, false},
		{"TEAM-1", "TEAM", 1, false},
		{"bad", "", 0, true},
		{"CRE-abc", "", 0, true},
	}

	for _, tt := range tests {
		team, number, err := parseIdentifier(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseIdentifier(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if team != tt.team || number != tt.number {
			t.Errorf("parseIdentifier(%q) = (%q, %d), want (%q, %d)", tt.input, team, number, tt.team, tt.number)
		}
	}
}

func TestGetTicket(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string `json:"query"`
			Variables struct {
				Filter struct {
					Number *struct {
						Eq int `json:"eq"`
					} `json:"number"`
					Team *struct {
						Key *struct {
							Eq string `json:"eq"`
						} `json:"key"`
					} `json:"team"`
				} `json:"filter"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}

		// Verify the filter uses number+team, not identifier.
		if req.Variables.Filter.Number == nil {
			t.Error("expected number filter, got nil")
		} else if req.Variables.Filter.Number.Eq != 99 {
			t.Errorf("expected number 99, got %d", req.Variables.Filter.Number.Eq)
		}
		if req.Variables.Filter.Team == nil || req.Variables.Filter.Team.Key == nil {
			t.Error("expected team.key filter, got nil")
		} else if req.Variables.Filter.Team.Key.Eq != "CM" {
			t.Errorf("expected team key CM, got %s", req.Variables.Filter.Team.Key.Eq)
		}

		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"issues": map[string]any{
					"nodes": []map[string]any{
						{
							"id":          "issue-uuid",
							"identifier":  "CM-99",
							"title":       "Implement feature X",
							"description": "details",
							"url":         "https://linear.app/cm/issue/CM-99",
							"state":       map[string]any{"name": "In Progress"},
							"labels":      map[string]any{"nodes": []any{}},
						},
					},
				},
			},
		})
	}

	c := newTestClient(t, handler)
	ticket, err := c.GetTicket(t.Context(), "CM-99")
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if ticket.Identifier != "CM-99" {
		t.Errorf("expected CM-99, got %s", ticket.Identifier)
	}
	if ticket.State.Name != "In Progress" {
		t.Errorf("expected 'In Progress', got %s", ticket.State.Name)
	}
}

func TestGetTicketNotFound(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"issues": map[string]any{
					"nodes": []any{},
				},
			},
		})
	}

	c := newTestClient(t, handler)
	_, err := c.GetTicket(t.Context(), "CM-999")
	if err == nil {
		t.Fatal("expected error for missing ticket")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestGraphQLError(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{
			"errors": []map[string]any{
				{"message": "Authentication required"},
			},
		})
	}

	c := newTestClient(t, handler)
	_, err := c.GetTicket(t.Context(), "CM-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Authentication required") {
		t.Errorf("expected auth error, got: %v", err)
	}
}

func TestHTTPError(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		writeJSON(t, w, "rate limited")
	}

	c := newTestClient(t, handler)
	_, err := c.GetTicket(t.Context(), "CM-1")
	if err == nil {
		t.Fatal("expected error for 429")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("expected 429 error, got: %v", err)
	}
}

func TestListChildren(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"issues": map[string]any{
					"nodes": []map[string]any{
						{"id": "child-1", "identifier": "CM-101", "title": "Child 1"},
						{"id": "child-2", "identifier": "CM-102", "title": "Child 2"},
					},
				},
			},
		})
	}

	c := newTestClient(t, handler)
	children, err := c.ListChildren(t.Context(), "parent-uuid")
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	if len(children) != 2 {
		t.Errorf("expected 2 children, got %d", len(children))
	}
}
