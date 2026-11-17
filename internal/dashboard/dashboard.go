// Package dashboard renders the page that links to every reachable Service.
package dashboard

import (
	"cmp"
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"

	"github.com/marcomontalbano/tailserve/internal/status"
)

//go:embed dashboard.html
var pageSource string

var page = template.Must(template.New("dashboard").Parse(pageSource))

// localOnlyGroupName titles the group of Services whose Repository has no remote, or that run outside any Repository.
const localOnlyGroupName = "Local only"

// pageData is what the Dashboard template renders.
type pageData struct {
	Groups []group
}

// group is the Services of one Repository owner, such as "marcomontalbano", or of localOnlyGroupName.
type group struct {
	Name  string
	Links []link
}

// link is one Service on the Dashboard.
type link struct {
	Port uint16
	// Name is the Repository name, else the folder name, else "<tool> on <port>".
	Name string
	// Location is the path inside the Repository, "/ (repo root)" at its root, or the folder outside any Repository.
	Location string
	// Tool is what the Service runs, such as "astro dev", or its Process name.
	Tool string
	// Folder is the full working folder with "~" for the home folder, which orders the links; empty when unknown.
	Folder string
	URL    string
	// RepositoryURL is the web page of the Repository; empty when it has no remote.
	RepositoryURL string
	// RepositoryHost is the host of RepositoryURL, such as "github.com", for the button's label.
	RepositoryHost string
	// RepositoryFullName is "<owner>/<name>" of the Repository, for the button's label.
	RepositoryFullName string
	// IsOutsideTailserve marks a Service that someone else made reachable.
	IsOutsideTailserve bool

	// groupName is the group the link belongs to.
	groupName string
}

// Handler serves the Dashboard for the Snapshot that currentSnapshot returns at each request.
// When isOnThisMachine, the page links each Service at localhost instead of its tailnet address.
func Handler(currentSnapshot func() status.Snapshot, isOnThisMachine bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/" {
			http.NotFound(w, request)
			return
		}
		snapshot := currentSnapshot()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = page.Execute(w, pageData{Groups: groupByOwner(reachableLinks(snapshot.Rows, isOnThisMachine))})
	})
}

// reachableLinks lists the Served Services of rows, sorted by folder and then
// port so the Services of one Repository sit together. When isOnThisMachine,
// each link opens the Service at localhost instead of its tailnet address.
func reachableLinks(rows []status.Row, isOnThisMachine bool) []link {
	var links []link
	for _, row := range rows {
		if row.State != status.Served && row.State != status.ServedOutsideTailserve {
			continue
		}
		service := row.Service
		serviceLink := link{
			Port:               service.Port,
			Name:               cmp.Or(service.FolderName(), fmt.Sprintf("%s on %d", service.ToolName(), service.Port)),
			Location:           service.ShortFolder(),
			Tool:               service.ToolName(),
			Folder:             service.ShortFolder(),
			URL:                row.Detail,
			IsOutsideTailserve: row.State == status.ServedOutsideTailserve,
			groupName:          localOnlyGroupName,
		}
		if isOnThisMachine {
			serviceLink.URL = fmt.Sprintf("http://localhost:%d/", service.Port)
		}
		if repository := service.Repository; repository != nil {
			serviceLink.Name = repository.Name
			serviceLink.Location = cmp.Or(service.PathInRepository(), "/ (repo root)")
			if repository.WebURL != "" {
				serviceLink.groupName = repository.Owner
				serviceLink.RepositoryURL = repository.WebURL
				serviceLink.RepositoryFullName = repository.Owner + "/" + repository.Name
				if repositoryURL, err := url.Parse(repository.WebURL); err == nil {
					serviceLink.RepositoryHost = repositoryURL.Host
				}
			}
		}
		links = append(links, serviceLink)
	}

	slices.SortFunc(links, func(a, b link) int {
		// Services with an unknown folder go last.
		if (a.Folder == "") != (b.Folder == "") {
			if a.Folder == "" {
				return 1
			}
			return -1
		}
		return cmp.Or(
			cmp.Compare(a.Folder, b.Folder),
			cmp.Compare(a.Port, b.Port),
		)
	})
	return links
}

// groupByOwner splits links by Repository owner, sorted by name, with the local only group last.
func groupByOwner(links []link) []group {
	var groups []group
	for _, serviceLink := range links {
		index := slices.IndexFunc(groups, func(existing group) bool { return existing.Name == serviceLink.groupName })
		if index < 0 {
			groups = append(groups, group{Name: serviceLink.groupName})
			index = len(groups) - 1
		}
		groups[index].Links = append(groups[index].Links, serviceLink)
	}
	slices.SortFunc(groups, func(a, b group) int {
		if (a.Name == localOnlyGroupName) != (b.Name == localOnlyGroupName) {
			if a.Name == localOnlyGroupName {
				return 1
			}
			return -1
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return groups
}
