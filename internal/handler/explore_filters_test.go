package handler

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// renderExplore parses the explore page the same way loadTemplates does
// (layouts + partials + the one page) and executes it with the given data.
func renderExplore(t *testing.T, data map[string]interface{}) string {
	t.Helper()

	root := repoRoot(t)
	templateDir := filepath.Join(root, "web", "templates")

	layouts, err := filepath.Glob(filepath.Join(templateDir, "layouts", "*.html"))
	if err != nil || len(layouts) == 0 {
		t.Fatalf("no layout templates under %s (err %v)", templateDir, err)
	}
	partials, _ := filepath.Glob(filepath.Join(templateDir, "partials", "*.html"))

	files := append(append([]string{}, layouts...), partials...)
	files = append(files, filepath.Join(templateDir, "pages", "explore.html"))

	tmpl, err := template.New("explore.html").Funcs(templateFuncs()).ParseFiles(files...)
	if err != nil {
		t.Fatalf("parse explore.html: %v", err)
	}

	// Keys RenderWithRequest always supplies.
	for k, v := range map[string]interface{}{"Path": "/explore", "CSRFToken": "", "MapStyleURL": ""} {
		if _, ok := data[k]; !ok {
			data[k] = v
		}
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "explore.html", data); err != nil {
		t.Fatalf("execute explore.html: %v", err)
	}
	return buf.String()
}

// repoRoot walks up from the package directory until it finds web/templates.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "web", "templates")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate web/templates above the package directory")
		}
		dir = parent
	}
}

func baseExploreData() map[string]interface{} {
	return map[string]interface{}{
		"Destinations": nil,
		"Total":        0,
		"Filter":       nil,
		"Categories":   []string{"nature"},
		"Regions":      []string{"Europe"},
		"AISuggest":    false,
	}
}

// best_for and best_months are populated on no destination, so offering the
// controls means offering a filter that can only ever return an empty list.
func TestExploreHidesFiltersWithNoDataBehindThem(t *testing.T) {
	data := baseExploreData()
	data["HasBestFor"] = false
	data["HasMonths"] = false

	html := renderExplore(t, data)

	if strings.Contains(html, "Best For") {
		t.Error("Best For control rendered even though no destination carries best_for")
	}
	if strings.Contains(html, "Travel Month") {
		t.Error("Travel Month control rendered even though no destination carries best_months")
	}
	// Controls that do have data must survive.
	if !strings.Contains(html, "Daily Budget") {
		t.Error("Daily Budget control went missing")
	}
}

// The controls come back on their own once the data exists — nothing here is
// a permanent removal.
func TestExploreShowsFiltersWhenDataExists(t *testing.T) {
	data := baseExploreData()
	data["HasBestFor"] = true
	data["HasMonths"] = true

	html := renderExplore(t, data)

	if !strings.Contains(html, "Best For") {
		t.Error("Best For control missing when best_for is populated")
	}
	if !strings.Contains(html, "Travel Month") {
		t.Error("Travel Month control missing when best_months is populated")
	}
}

// A hidden control leaves no way to clear the filter, so a deep link like
// ?best_for=couples must not be restored into Alpine state either.
func TestExploreMarksHiddenFiltersUnavailableToAlpine(t *testing.T) {
	data := baseExploreData()
	data["HasBestFor"] = false
	data["HasMonths"] = false

	html := renderExplore(t, data)

	for _, want := range []string{"'best_for'", "'month'"} {
		if !strings.Contains(html, want) {
			t.Errorf("unavailableFilters is missing %s", want)
		}
	}

	// With both populated the list must be empty, or init() would skip
	// restoring filters that are perfectly usable.
	shown := renderExplore(t, func() map[string]interface{} {
		d := baseExploreData()
		d["HasBestFor"] = true
		d["HasMonths"] = true
		return d
	}())

	start := strings.Index(shown, "unavailableFilters: [")
	if start < 0 {
		t.Fatal("unavailableFilters not found")
	}
	end := strings.Index(shown[start:], "]")
	if end < 0 {
		t.Fatal("unterminated unavailableFilters array")
	}
	if body := strings.TrimSpace(shown[start+len("unavailableFilters: [") : start+end]); body != "" {
		t.Errorf("unavailableFilters = %q, want empty when both filters have data", body)
	}
}
