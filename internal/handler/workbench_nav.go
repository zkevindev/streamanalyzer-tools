package handler

import (
	"bytes"
	_ "embed"
	"html/template"
	"io/fs"
	"strings"
)

//go:embed workbench_nav.html
var workbenchNavHTML string

var workbenchNav = template.Must(template.New("workbench-nav").Parse(workbenchNavHTML))

// Workbench pages share the navigation markup. Render it once from the embedded
// assets so links and their current-page states are available before scripts run.
func renderWorkbenchPages(root fs.FS) map[string][]byte {
	pages := make(map[string][]byte)
	files, err := fs.Glob(root, "*.html")
	if err != nil {
		panic(err)
	}
	for _, name := range files {
		document, err := fs.ReadFile(root, name)
		if err != nil {
			panic(err)
		}
		if !bytes.Contains(document, []byte("<!-- workbench-header -->")) {
			continue
		}
		page := strings.TrimSuffix(name, ".html")
		state := struct {
			Page        string
			Simulator   bool
			Diagnostics bool
		}{
			Page:        page,
			Simulator:   page == "simulators" || strings.HasPrefix(page, "simulator-"),
			Diagnostics: page == "diagnostics" || page == "realtime" || page == "offline" || page == "hls-stats",
		}
		for _, section := range []struct{ marker, template string }{
			{"<!-- workbench-header -->", "header"},
			{"<!-- diagnostic-tools -->", "diagnostic-tools"},
			{"<!-- stream-modes -->", "stream-modes"},
			{"<!-- review-modes -->", "review-modes"},
			{"<!-- simulator-protocols -->", "simulator-protocols"},
		} {
			marker := []byte(section.marker)
			if !bytes.Contains(document, marker) {
				continue
			}
			var markup bytes.Buffer
			if err := workbenchNav.ExecuteTemplate(&markup, section.template, state); err != nil {
				panic(err)
			}
			document = bytes.Replace(document, marker, markup.Bytes(), 1)
		}
		pages[name] = document
	}
	return pages
}
