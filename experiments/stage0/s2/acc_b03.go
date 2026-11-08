package s2

func accB03() Task {
	return Task{
		ID:     "s2-b03-url-router",
		Set:    "acceptance",
		System: system("a small URL router"),
		Files: map[string]string{
			"go.mod": goMod,
			"router.go": `package router

// Route maps a method and a path pattern to a name. Pattern segments are separated
// by "/", and a segment written as :name captures one path segment.
type Route struct {
	Method  string
	Pattern string
	Name    string
}

// Router holds registered routes.
type Router struct {
	routes []Route
}

// Add registers a route.
func (r *Router) Add(rt Route) { r.routes = append(r.routes, rt) }

// Len reports how many routes there are.
func (r *Router) Len() int { return len(r.routes) }
`,
			"docs/gateway.txt": `Gateway notes

The gateway forwards paths as clients sent them, and clients are inconsistent
about a slash at the end. /users/ has to reach the same route as /users, and
/users/7/ the same route as /users/7, with the same captured values.

The gateway adds no prefix of its own.
`,
			"router_test.go": `package router

import "testing"

func TestAddLen(t *testing.T) {
	var r Router
	r.Add(Route{Method: "GET", Pattern: "/a", Name: "a"})
	if r.Len() != 1 {
		t.Fatalf("Len = %d", r.Len())
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package router_test

import (
	"errors"
	"slices"
	"testing"

	router "fixture"
)

func route(method, pattern, name string) router.Route {
	return router.Route{Method: method, Pattern: pattern, Name: name}
}

func build(routes ...router.Route) *router.Router {
	r := &router.Router{}
	for _, rt := range routes {
		r.Add(rt)
	}
	return r
}

func TestNamesInOrderAdded(t *testing.T) {
	r := build(route("GET", "/b", "b"), route("GET", "/a", "a"))
	if got := r.Names(); !slices.Equal(got, []string{"b", "a"}) {
		t.Fatalf("Names = %v", got)
	}
}

func TestMatchExact(t *testing.T) {
	got, params, err := build(route("GET", "/a/b", "ab")).Match("GET", "/a/b")
	if err != nil || got.Name != "ab" || len(params) != 0 {
		t.Fatalf("got %+v, %v, %v", got, params, err)
	}
}

func TestMatchCaptures(t *testing.T) {
	got, params, err := build(route("GET", "/users/:id/posts/:post", "p")).Match("GET", "/users/7/posts/9")
	if err != nil || got.Name != "p" || params["id"] != "7" || params["post"] != "9" {
		t.Fatalf("got %+v, %v, %v", got, params, err)
	}
}

func TestMatchLiteralAddedLater(t *testing.T) {
	r := build(route("GET", "/users/:id", "user"), route("GET", "/users/me", "me"))
	got, _, err := r.Match("GET", "/users/me")
	if err != nil || got.Name != "me" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestMatchFirstDifferingSegment(t *testing.T) {
	r := build(route("GET", "/a/:x/c", "first"), route("GET", "/a/b/:y", "second"))
	got, _, err := r.Match("GET", "/a/b/c")
	if err != nil || got.Name != "second" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestMatchParamStillServes(t *testing.T) {
	r := build(route("GET", "/users/me", "me"), route("GET", "/users/:id", "user"))
	got, params, err := r.Match("GET", "/users/7")
	if err != nil || got.Name != "user" || params["id"] != "7" {
		t.Fatalf("got %+v, %v, %v", got, params, err)
	}
}

func TestMatchSlashAtEnd(t *testing.T) {
	got, _, err := build(route("GET", "/users", "users")).Match("GET", "/users/")
	if err != nil || got.Name != "users" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestMatchSlashAtEndCapture(t *testing.T) {
	r := build(route("GET", "/users/:id", "user"))
	got, params, err := r.Match("GET", "/users/7/")
	if err != nil || got.Name != "user" || params["id"] != "7" {
		t.Fatalf("got %+v, %v, %v", got, params, err)
	}
	if _, _, err := r.Match("GET", "/users/"); err == nil {
		t.Fatal("/users/ is /users, which has no route here")
	}
}

func TestAllowedSlashAtEnd(t *testing.T) {
	r := build(route("POST", "/users", "users"))
	if got := r.Allowed("/users/"); !slices.Equal(got, []string{"POST"}) {
		t.Fatalf("Allowed = %v", got)
	}
}

func TestMatchUnknownPath(t *testing.T) {
	_, _, err := build(route("GET", "/a", "a")).Match("GET", "/b")
	if !errors.Is(err, router.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestMatchWrongMethod(t *testing.T) {
	_, _, err := build(route("POST", "/a", "a")).Match("GET", "/a")
	if !errors.Is(err, router.ErrMethodNotAllowed) {
		t.Fatalf("err = %v", err)
	}
}

func TestMatchHeadUsesGet(t *testing.T) {
	got, _, err := build(route("GET", "/a", "a")).Match("HEAD", "/a")
	if err != nil || got.Name != "a" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestMatchHeadPrefersOwn(t *testing.T) {
	r := build(route("GET", "/a", "get"), route("HEAD", "/a", "head"))
	got, _, err := r.Match("HEAD", "/a")
	if err != nil || got.Name != "head" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestAllowedSorted(t *testing.T) {
	r := build(route("POST", "/a", "p"), route("DELETE", "/a", "d"), route("PUT", "/b", "other"))
	if got := r.Allowed("/a"); !slices.Equal(got, []string{"DELETE", "POST"}) {
		t.Fatalf("Allowed = %v", got)
	}
}

func TestAllowedUnknownPath(t *testing.T) {
	if got := build(route("GET", "/a", "a")).Allowed("/b"); len(got) != 0 {
		t.Fatalf("Allowed = %v", got)
	}
}

func TestAllowedIncludesHead(t *testing.T) {
	if got := build(route("GET", "/a", "a")).Allowed("/a"); !slices.Equal(got, []string{"GET", "HEAD"}) {
		t.Fatalf("Allowed = %v", got)
	}
}

func TestAllowedHeadInSortOrder(t *testing.T) {
	r := build(route("POST", "/a", "p"), route("GET", "/a", "g"), route("DELETE", "/a", "d"))
	if got := r.Allowed("/a"); !slices.Equal(got, []string{"DELETE", "GET", "HEAD", "POST"}) {
		t.Fatalf("Allowed = %v", got)
	}
}

func TestPathFills(t *testing.T) {
	r := build(route("GET", "/users/:id/posts", "posts"))
	got, err := r.Path("posts", map[string]string{"id": "7"})
	if err != nil || got != "/users/7/posts" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestPathMissingParameter(t *testing.T) {
	if _, err := build(route("GET", "/users/:id", "user")).Path("user", nil); err == nil {
		t.Fatal("no error")
	}
}

func TestPathUnknownNameError(t *testing.T) {
	_, err := build(route("GET", "/a", "a")).Path("nope", nil)
	if !errors.Is(err, router.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoveDrops(t *testing.T) {
	r := build(route("GET", "/a", "a"), route("GET", "/b", "b"))
	if err := r.Remove("a"); err != nil {
		t.Fatal(err)
	}
	if r.Len() != 1 {
		t.Fatalf("Len = %d", r.Len())
	}
	if _, _, err := r.Match("GET", "/b"); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveUnknownNameError(t *testing.T) {
	r := build(route("GET", "/a", "a"))
	if err := r.Remove("nope"); !errors.Is(err, router.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if r.Len() != 1 {
		t.Fatalf("Len = %d", r.Len())
	}
}
`},
		Solution: map[string]string{
			"router.go": `package router

import (
	"errors"
	"maps"
	"slices"
	"strings"
)

// ErrNotFound is for whatever can't find a route, by path or by name.
var ErrNotFound = errors.New("route not found")

// ErrMethodNotAllowed is for a path that has routes, just not for this method.
var ErrMethodNotAllowed = errors.New("method not allowed")

// Route maps a method and a path pattern to a name. Pattern segments are separated
// by "/", and a segment written as :name captures one path segment.
type Route struct {
	Method  string
	Pattern string
	Name    string
}

// Router holds registered routes.
type Router struct {
	routes []Route
}

// Add registers a route.
func (r *Router) Add(rt Route) { r.routes = append(r.routes, rt) }

// Len reports how many routes there are.
func (r *Router) Len() int { return len(r.routes) }

// Names returns the route names in the order they were added.
func (r *Router) Names() []string {
	out := make([]string, 0, len(r.routes))
	for _, rt := range r.routes {
		out = append(out, rt.Name)
	}
	return out
}

func split(p string) []string { return strings.Split(strings.Trim(p, "/"), "/") }

// capture matches a path against a pattern.
func capture(pattern, path string) (map[string]string, bool) {
	ps, segs := split(pattern), split(path)
	if len(ps) != len(segs) {
		return nil, false
	}
	params := map[string]string{}
	for i, s := range ps {
		if key, ok := strings.CutPrefix(s, ":"); ok {
			params[key] = segs[i]
		} else if s != segs[i] {
			return nil, false
		}
	}
	return params, true
}

// better reports whether a beats b, two patterns fitting the same path.
func better(a, b string) bool {
	as, bs := split(a), split(b)
	for i := range as {
		ap, bp := strings.HasPrefix(as[i], ":"), strings.HasPrefix(bs[i], ":")
		if ap != bp {
			return bp
		}
	}
	return false
}

func (r *Router) find(method, path string) (Route, map[string]string, bool) {
	var best Route
	var params map[string]string
	found := false
	for _, rt := range r.routes {
		if rt.Method != method {
			continue
		}
		got, ok := capture(rt.Pattern, path)
		if ok && (!found || better(rt.Pattern, best.Pattern)) {
			best, params, found = rt, got, true
		}
	}
	return best, params, found
}

// Match finds the route for a request and its captured values.
func (r *Router) Match(method, path string) (Route, map[string]string, error) {
	rt, params, ok := r.find(method, path)
	if !ok && method == "HEAD" {
		rt, params, ok = r.find("GET", path)
	}
	switch {
	case ok:
		return rt, params, nil
	case len(r.Allowed(path)) > 0:
		return Route{}, nil, ErrMethodNotAllowed
	}
	return Route{}, nil, ErrNotFound
}

// Allowed returns the methods a path can be requested with, sorted.
func (r *Router) Allowed(path string) []string {
	seen := map[string]bool{}
	for _, rt := range r.routes {
		if _, ok := capture(rt.Pattern, path); ok {
			seen[rt.Method] = true
			if rt.Method == "GET" {
				seen["HEAD"] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// Path builds the path of the named route.
func (r *Router) Path(name string, params map[string]string) (string, error) {
	for _, rt := range r.routes {
		if rt.Name != name {
			continue
		}
		segs := split(rt.Pattern)
		for i, s := range segs {
			if key, ok := strings.CutPrefix(s, ":"); ok {
				v, ok := params[key]
				if !ok {
					return "", errors.New("missing parameter " + key)
				}
				segs[i] = v
			}
		}
		return "/" + strings.Join(segs, "/"), nil
	}
	return "", ErrNotFound
}

// Remove takes the named route out.
func (r *Router) Remove(name string) error {
	for i, rt := range r.routes {
		if rt.Name == name {
			r.routes = slices.Delete(r.routes, i, i+1)
			return nil
		}
	}
	return ErrNotFound
}
`,
		},
		Turns: script(
			local("Read docs/gateway.txt and tell me what it asks of the router. No code changes."),
			local("Decision on overlapping routes: when more than one pattern fits a path, a literal segment beats a :param at the first segment where they differ, whatever order they were registered in, so /users/me wins over /users/:id. Add `func (r *Router) Names() []string`, the route names in the order they were added."),
			cloud("Add `func (r *Router) Match(method, path string) (Route, map[string]string, error)`: the route for a request and its captured values by parameter name, without the colon. Also add `var ErrNotFound = errors.New(\"route not found\")` and `var ErrMethodNotAllowed = errors.New(\"method not allowed\")`. The first is for whatever can't find a route, by path or by name. The second is for a path that has routes, just not for this method."),
			cloud("HEAD should work without anyone declaring it: a HEAD request is served by the GET route of that path, unless the path has a HEAD route of its own. Change Match to do that."),
			local("Add `func (r *Router) Allowed(path string) []string`: the methods this path can be requested with, sorted. An unknown path gives an empty result."),
			local("Add `func (r *Router) Path(name string, params map[string]string) (string, error)`: builds the path of the named route, filling each :param from params. A missing parameter or a name no route has is an error."),
			cloud("Add `func (r *Router) Remove(name string) error`, which takes the named route out. A name no route has is an error."),
		),
		Carries: []Carry{
			{
				What:        "a literal segment beats a parameter at the first difference, whatever the registration order",
				Kind:        CarryUser,
				Established: 1,
				Needed:      []int{2},
				Tests:       []string{"TestMatchLiteralAddedLater", "TestMatchFirstDifferingSegment"},
				Absent:      []string{"literal", "beats", "wins over", "registered in"},
			},
			{
				What:        "a slash at the end of a path is ignored (docs/gateway.txt)",
				Kind:        CarryDiscovered,
				Established: 0,
				Needed:      []int{2, 4},
				Source:      "docs/gateway.txt",
				Tests:       []string{"TestMatchSlashAtEnd", "TestMatchSlashAtEndCapture", "TestAllowedSlashAtEnd"},
				Absent:      []string{"trailing", "slash", "same route as"},
			},
			{
				What:        "a GET route also serves HEAD, so such a path can be requested with HEAD",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{4},
				Tests:       []string{"TestAllowedIncludesHead", "TestAllowedHeadInSortOrder"},
				Absent:      []string{"head", "get route"},
			},
			{
				What:        "ErrNotFound is for whatever can't find a route, by path or by name",
				Kind:        CarryEmbodied,
				Established: 2,
				Needed:      []int{5, 6},
				Tests:       []string{"TestPathUnknownNameError", "TestRemoveUnknownNameError"},
				Absent:      []string{"errnotfound", "sentinel"},
			},
		},
	}
}
