package wepi

import (
	"regexp"
	"testing"
)

func TestBuildRegexFromTemplate(t *testing.T) {
	re, keys := buildRegexFromTemplate("/users/{id}")
	if re == nil {
		t.Fatal("expected non-nil regex")
	}
	if len(keys) != 1 || keys[0] != "id" {
		t.Errorf("keys = %v, want [id]", keys)
	}
	if !re.MatchString("/users/123") {
		t.Error("regex should match /users/123")
	}
	if re.MatchString("/users/123/extra") {
		t.Error("regex should not match /users/123/extra")
	}
}

func TestBuildRegexFromTemplate_NoParams(t *testing.T) {
	re, keys := buildRegexFromTemplate("/static/path")
	if re != nil || keys != nil {
		t.Error("expected nil for template with no params")
	}
}

func TestExtractPatternValues(t *testing.T) {
	re := regexp.MustCompile(`^/users/([^/]+)/posts/([^/]+)$`)
	keys := []string{"userId", "postId"}

	values := extractPatternValues(re, keys, "/users/alice/posts/42")
	if values == nil {
		t.Fatal("expected non-nil values")
	}
	if values["userId"] != "alice" {
		t.Errorf("userId = %q, want %q", values["userId"], "alice")
	}
	if values["postId"] != "42" {
		t.Errorf("postId = %q, want %q", values["postId"], "42")
	}
}

func TestExtractPatternValues_NoMatch(t *testing.T) {
	re := regexp.MustCompile(`^/users/([^/]+)$`)
	keys := []string{"id"}

	values := extractPatternValues(re, keys, "/other/path")
	if values != nil {
		t.Error("expected nil for non-matching path")
	}
}

func TestLoadRouteFromRequest(t *testing.T) {
	w := Get()

	route := &Route{
		route:  "/users/{id}",
		method: GET,
	}
	w.addRoute(&WepiComposedRoute{
		path:   "/users/{id}",
		route:  route,
		method: GET,
	})

	path, r, params := w.loadRouteFromRequest("/users/42", GET)
	if path == "" {
		t.Fatal("expected a match")
	}
	if r != route {
		t.Error("expected the registered route")
	}
	if params["id"] != "42" {
		t.Errorf("id = %q, want %q", params["id"], "42")
	}

	path, _, _ = w.loadRouteFromRequest("/users/42", POST)
	if path != "" {
		t.Error("expected no match for wrong method")
	}
}

func TestLoadRouteFromRequest_LiteralBeatsPattern(t *testing.T) {
	w := Get()

	pattern := &Route{route: "/device/{id}/status", method: GET}
	w.addRoute(&WepiComposedRoute{path: "/device/{id}/status", route: pattern, method: GET})
	w.addPattern("/device/{id}/status")

	literal := &Route{route: "/device/pin/status", method: POST}
	w.addRoute(&WepiComposedRoute{path: "/device/pin/status", route: literal, method: POST})

	// The literal POST route must not be shadowed by the GET pattern capturing id="pin"
	path, r, params := w.loadRouteFromRequest("/device/pin/status", POST)
	if r != literal {
		t.Fatalf("expected the literal route, got %v (path %q)", r, path)
	}
	if params != nil {
		t.Errorf("expected no pattern params, got %v", params)
	}

	// The pattern still serves every other id
	_, r, params = w.loadRouteFromRequest("/device/abc/status", GET)
	if r != pattern || params["id"] != "abc" {
		t.Errorf("expected the pattern route with id=abc, got %v %v", r, params)
	}
}
