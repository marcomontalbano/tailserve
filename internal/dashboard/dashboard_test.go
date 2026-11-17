package dashboard

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marcomontalbano/tailserve/internal/discovery"
	"github.com/marcomontalbano/tailserve/internal/status"
)

// websiteRepository has a GitHub remote and holds the folder /Users/me/marcomontalbano.github.io/packages/website.
var websiteRepository = &discovery.Repository{Root: "/Users/me/marcomontalbano.github.io", Owner: "marcomontalbano", Name: "marcomontalbano.github.io", WebURL: "https://github.com/marcomontalbano/marcomontalbano.github.io"}

// scratchRepository has no remote.
var scratchRepository = &discovery.Repository{Root: "/Users/me/scratch", Name: "scratch"}

func TestReachableLinksAndGroups(t *testing.T) {
	t.Setenv("HOME", "/Users/me")
	rows := []status.Row{
		{Service: discovery.Service{Port: 9000, ProcessName: "python3"}, State: status.Served, Detail: "https://m:9000"},
		{Service: discovery.Service{Port: 5174, ProcessName: "node", Tool: "vite", Folder: "/Users/me/marcomontalbano.github.io/packages/website", Repository: websiteRepository}, State: status.Served, Detail: "https://m:5174"},
		{Service: discovery.Service{Port: 3000, ProcessName: "ruby", Folder: "/Users/me/blog"}, State: status.NotAllowed},
		{Service: discovery.Service{Port: 5173, ProcessName: "node", Tool: "astro dev", Folder: "/Users/me/marcomontalbano.github.io", Repository: websiteRepository}, State: status.ServedOutsideTailserve, Detail: "https://m:5173"},
		{Service: discovery.Service{Port: 8080, ProcessName: "bun", Folder: "/Users/me/scratch", Repository: scratchRepository}, State: status.Served, Detail: "https://m:8080"},
		{Service: discovery.Service{Port: 3001, ProcessName: "node", Tool: "serve", Folder: "/Users/me/poc"}, State: status.Served, Detail: "https://m:3001"},
	}

	groups := groupByOwner(reachableLinks(rows, false))

	want := []group{
		{Name: "marcomontalbano", Links: []link{
			{Port: 5173, Name: "marcomontalbano.github.io", Location: "/ (repo root)", Tool: "astro dev", Folder: "~/marcomontalbano.github.io", URL: "https://m:5173", RepositoryURL: "https://github.com/marcomontalbano/marcomontalbano.github.io", RepositoryHost: "github.com", RepositoryFullName: "marcomontalbano/marcomontalbano.github.io", IsOutsideTailserve: true, groupName: "marcomontalbano"},
			{Port: 5174, Name: "marcomontalbano.github.io", Location: "packages/website", Tool: "vite", Folder: "~/marcomontalbano.github.io/packages/website", URL: "https://m:5174", RepositoryURL: "https://github.com/marcomontalbano/marcomontalbano.github.io", RepositoryHost: "github.com", RepositoryFullName: "marcomontalbano/marcomontalbano.github.io", groupName: "marcomontalbano"},
		}},
		{Name: localOnlyGroupName, Links: []link{
			{Port: 3001, Name: "poc", Location: "~/poc", Tool: "serve", Folder: "~/poc", URL: "https://m:3001", groupName: localOnlyGroupName},
			{Port: 8080, Name: "scratch", Location: "/ (repo root)", Tool: "bun", Folder: "~/scratch", URL: "https://m:8080", groupName: localOnlyGroupName},
			{Port: 9000, Name: "python3 on 9000", Tool: "python3", URL: "https://m:9000", groupName: localOnlyGroupName},
		}},
	}
	if len(groups) != len(want) {
		t.Fatalf("got %d groups, want %d: %+v", len(groups), len(want), groups)
	}
	for groupIndex := range want {
		if groups[groupIndex].Name != want[groupIndex].Name || len(groups[groupIndex].Links) != len(want[groupIndex].Links) {
			t.Fatalf("group %d = %+v, want %+v", groupIndex, groups[groupIndex], want[groupIndex])
		}
		for linkIndex := range want[groupIndex].Links {
			if got := groups[groupIndex].Links[linkIndex]; got != want[groupIndex].Links[linkIndex] {
				t.Errorf("group %q link %d =\n%+v\nwant\n%+v", want[groupIndex].Name, linkIndex, got, want[groupIndex].Links[linkIndex])
			}
		}
	}
}

func TestHandlerRendersLinks(t *testing.T) {
	t.Setenv("HOME", "/Users/me")
	snapshot := status.Snapshot{Hostname: "machine.tailnet.ts.net", Rows: []status.Row{
		{Service: discovery.Service{Port: 5173, ProcessName: "node", Tool: "vite", Folder: "/Users/me/marcomontalbano.github.io/packages/website", Repository: websiteRepository}, State: status.Served, Detail: "https://m:5173"},
	}}
	recorder := httptest.NewRecorder()
	Handler(func() status.Snapshot { return snapshot }, false).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))

	body := recorder.Body.String()
	for _, expected := range []string{
		`<title>tailserve</title>`,
		`<h2 id="group-0">marcomontalbano<span class="count">1</span></h2>`,
		`<a class="service" href="https://m:5173" target="_blank" rel="noopener">`,
		`<span class="visually-hidden">(opens in a new tab)</span>`,
		`<span class="name">marcomontalbano.github.io</span>`,
		`<span class="location">packages/website</span>`,
		`<span class="port">5173</span>`,
		`<span class="tool">vite</span>`,
		`href="https://github.com/marcomontalbano/marcomontalbano.github.io" target="_blank" rel="noopener" aria-label="Open marcomontalbano/marcomontalbano.github.io on github.com (opens in a new tab)"`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("the page is missing %q:\n%s", expected, body)
		}
	}
}

func TestHandlerRendersEmptyDashboard(t *testing.T) {
	recorder := httptest.NewRecorder()
	Handler(func() status.Snapshot { return status.Snapshot{} }, false).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))

	body := recorder.Body.String()
	for _, expected := range []string{
		`<title>tailserve</title>`,
		"No Service is served right now.",
		`<a class="project" href="https://github.com/marcomontalbano/tailserve" target="_blank" rel="noopener">`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("the empty page is missing %q", expected)
		}
	}
}

func TestHandlerOnThisMachineLinksToLocalhost(t *testing.T) {
	snapshot := status.Snapshot{Rows: []status.Row{
		{Service: discovery.Service{Port: 5173, ProcessName: "node", Tool: "vite"}, State: status.Served, Detail: "https://m:5173"},
		{Service: discovery.Service{Port: 4321, ProcessName: "node", Tool: "astro dev"}, State: status.ServedOutsideTailserve, Detail: "https://m:8443"},
	}}
	recorder := httptest.NewRecorder()
	Handler(func() status.Snapshot { return snapshot }, true).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))

	body := recorder.Body.String()
	for _, expected := range []string{`href="http://localhost:5173/"`, `href="http://localhost:4321/"`} {
		if !strings.Contains(body, expected) {
			t.Errorf("the page on this machine is missing %q", expected)
		}
	}
	if strings.Contains(body, "https://m:") {
		t.Error("the page on this machine links to a tailnet address")
	}
}
