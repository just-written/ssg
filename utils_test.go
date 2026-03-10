package main

import (
	"encoding/json"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isWithinDir
func TestIsWithinDir(t *testing.T) {
	cases := []struct {
		root, target string
		want         bool
	}{
		{"/build", "/build/index.html", true},
		{"/build", "/build/sub/page.html", true},
		{"/build", "/build", true},
		{"/build", "/other/page.html", false},
		{"/build", "/build/../etc/passwd", false},
	}
	for _, c := range cases {
		got := isWithinDir(c.root, c.target)
		if got != c.want {
			t.Errorf("isWithinDir(%q, %q) = %v, want %v", c.root, c.target, got, c.want)
		}
	}
}

// readJSONFile / readJSONFileOptional
func TestReadJSONFile_ValidFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.json")
	writeFile(t, path, `{"key":"value"}`)

	data, err := readJSONFile(path)
	if err != nil {
		t.Fatalf("readJSONFile: %v", err)
	}
	if data["key"] != "value" {
		t.Errorf("expected 'value', got %v", data["key"])
	}
}

func TestReadJSONFile_MissingFile(t *testing.T) {
	if _, err := readJSONFile("/no/such/file.json"); err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestReadJSONFile_InvalidJSON(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bad.json")
	writeFile(t, path, `{not valid json`)

	if _, err := readJSONFile(path); err == nil {
		t.Error("expected parse error, got nil")
	}
}

func TestReadJSONFileOptional_MissingReturnsNil(t *testing.T) {
	data, err := readJSONFileOptional("/no/such/file.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil data for missing file, got %v", data)
	}
}

// hasLayout
func TestHasLayout(t *testing.T) {
	root := t.TempDir()

	emptyPath := filepath.Join(root, "empty.html")
	writeFile(t, emptyPath, "   \n  ")
	if hasLayout(emptyPath) {
		t.Error("expected hasLayout=false for whitespace-only file")
	}

	contentPath := filepath.Join(root, "layout.html")
	writeFile(t, contentPath, "<html></html>")
	if !hasLayout(contentPath) {
		t.Error("expected hasLayout=true for non-empty file")
	}

	if hasLayout(filepath.Join(root, "nonexistent.html")) {
		t.Error("expected hasLayout=false for missing file")
	}
}

// resolveOutputPath
func TestResolveOutputPath(t *testing.T) {
	pagesDir := "/src/pages"
	buildDir := "/dist"

	out, err := resolveOutputPath(pagesDir, buildDir, "/src/pages/about/index.html")
	if err != nil {
		t.Fatalf("resolveOutputPath: %v", err)
	}
	want := filepath.Join("/dist", "about", "index.html")
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// copyDir
func TestCopyDir_ReproducesStructure(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	writeFile(t, filepath.Join(src, "a.txt"), "hello")
	writeFile(t, filepath.Join(src, "sub", "b.txt"), "world")

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	assertFileExists(t, filepath.Join(dst, "a.txt"))
	assertFileExists(t, filepath.Join(dst, "sub", "b.txt"))

	if readFile(t, filepath.Join(dst, "a.txt")) != "hello" {
		t.Error("a.txt content mismatch")
	}
	if readFile(t, filepath.Join(dst, "sub", "b.txt")) != "world" {
		t.Error("sub/b.txt content mismatch")
	}
}

// validateAssetRefs
func TestValidateAssetRefs_MissingAsset(t *testing.T) {
	manifest := map[string]string{"style.css": "/assets/style.abc123.css"}
	funcMap := template.FuncMap{
		"asset": func(s string) (string, error) { return s, nil },
	}
	tmpl, err := template.New("test.html").Funcs(funcMap).Parse(`<link href="{{asset "missing.css"}}">`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := validateAssetRefs(tmpl, manifest); err == nil {
		t.Error("expected error for missing asset ref, got nil")
	}
}

func TestValidateAssetRefs_PresentAsset(t *testing.T) {
	manifest := map[string]string{"style.css": "/assets/style.abc123.css"}
	funcMap := template.FuncMap{
		"asset": func(s string) (string, error) { return s, nil },
	}
	tmpl, err := template.New("test.html").Funcs(funcMap).Parse(`<link href="{{asset "style.css"}}">`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := validateAssetRefs(tmpl, manifest); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// checkBrokenLinks
func TestCheckBrokenLinks_DetectsBroken(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "index.html"), `<a href="/missing.html">broken</a>`)

	count, err := checkBrokenLinks(root)
	if err != nil {
		t.Fatalf("checkBrokenLinks: %v", err)
	}
	if count == 0 {
		t.Error("expected broken link count > 0")
	}
}

func TestCheckBrokenLinks_IgnoresExternal(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "index.html"),
		`<a href="https://example.com/page">ext</a><a href="//cdn.example.com/js">cdn</a>`)

	count, err := checkBrokenLinks(root)
	if err != nil {
		t.Fatalf("checkBrokenLinks: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 broken links for external URLs, got %d", count)
	}
}

func TestCheckBrokenLinks_IgnoresAnchors(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "index.html"), `<a href="#section">anchor</a>`)

	count, err := checkBrokenLinks(root)
	if err != nil {
		t.Fatalf("checkBrokenLinks: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for anchor-only links, got %d", count)
	}
}

func TestCheckBrokenLinks_IndexHTMLResolution(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "index.html"), `<a href="/about/">about</a>`)
	writeFile(t, filepath.Join(root, "about", "index.html"), `<p>about</p>`)

	count, err := checkBrokenLinks(root)
	if err != nil {
		t.Fatalf("checkBrokenLinks: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 broken links when index.html exists, got %d", count)
	}
}

// copyAssets
func TestCopyAssets_HashChangesWithContent(t *testing.T) {
	root1 := t.TempDir()
	writeFile(t, filepath.Join(root1, "assets", "app.js"), "console.log('v1')")
	out1 := filepath.Join(root1, "out1")
	if err := os.MkdirAll(out1, 0755); err != nil {
		t.Fatal(err)
	}
	m1, _, err := copyAssets(root1, out1, true)
	if err != nil {
		t.Fatal(err)
	}

	root2 := t.TempDir()
	writeFile(t, filepath.Join(root2, "assets", "app.js"), "console.log('v2')")
	out2 := filepath.Join(root2, "out2")
	if err := os.MkdirAll(out2, 0755); err != nil {
		t.Fatal(err)
	}
	m2, _, err := copyAssets(root2, out2, true)
	if err != nil {
		t.Fatal(err)
	}

	if m1["app.js"] == m2["app.js"] {
		t.Error("expected different hashed URLs for different file contents")
	}
}

func TestCopyAssets_SameContentSameHash(t *testing.T) {
	content := "body { color: blue; }"

	mkProject := func() map[string]string {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "assets", "style.css"), content)
		out := filepath.Join(root, "out")
		if err := os.MkdirAll(out, 0755); err != nil {
			t.Fatal(err)
		}
		m, _, err := copyAssets(root, out, true)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}

	m1 := mkProject()
	m2 := mkProject()

	if m1["style.css"] != m2["style.css"] {
		t.Errorf("same content should produce same hash: %q vs %q", m1["style.css"], m2["style.css"])
	}
}

// loadPageData
func TestLoadPageData_MergePrecedence(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "pages")

	globalData, _ := json.Marshal(map[string]any{"title": "Global", "site": "MySite"})
	writeFile(t, filepath.Join(pagesDir, "_data.json"), string(globalData))

	pageJSON, _ := json.Marshal(map[string]any{"title": "PageTitle"})
	writeFile(t, filepath.Join(pagesDir, "index.json"), string(pageJSON))
	writeFile(t, filepath.Join(pagesDir, "index.html"), `{{.title}}`)

	data, _, err := loadPageData(pagesDir, filepath.Join(pagesDir, "index.html"))
	if err != nil {
		t.Fatalf("loadPageData: %v", err)
	}
	if data["title"] != "PageTitle" {
		t.Errorf("expected page-level title to win, got %v", data["title"])
	}
	if data["site"] != "MySite" {
		t.Errorf("expected global site key to survive, got %v", data["site"])
	}
}

func TestLoadPageData_SubdirectoryDataInherited(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "pages")

	writeFile(t, filepath.Join(pagesDir, "_data.json"), `{"site":"MySite"}`)
	writeFile(t, filepath.Join(pagesDir, "blog", "_data.json"), `{"section":"Blog"}`)
	writeFile(t, filepath.Join(pagesDir, "blog", "posts", "my-post.json"), `{"title":"My Post"}`)
	writeFile(t, filepath.Join(pagesDir, "blog", "posts", "my-post.html"), `{{.title}}`)

	data, _, err := loadPageData(pagesDir, filepath.Join(pagesDir, "blog", "posts", "my-post.html"))
	if err != nil {
		t.Fatalf("loadPageData: %v", err)
	}
	if data["site"] != "MySite" {
		t.Errorf("expected global 'site' key, got %v", data["site"])
	}
	if data["section"] != "Blog" {
		t.Errorf("expected blog-level 'section' key, got %v", data["section"])
	}
	if data["title"] != "My Post" {
		t.Errorf("expected page-level 'title' key, got %v", data["title"])
	}
}

func TestLoadPageData_SubdirectoryDataOverridesGlobal(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "pages")

	writeFile(t, filepath.Join(pagesDir, "_data.json"), `{"author":"Global"}`)
	writeFile(t, filepath.Join(pagesDir, "blog", "_data.json"), `{"author":"BlogAuthor"}`)
	writeFile(t, filepath.Join(pagesDir, "blog", "index.html"), `{{.author}}`)

	data, _, err := loadPageData(pagesDir, filepath.Join(pagesDir, "blog", "index.html"))
	if err != nil {
		t.Fatalf("loadPageData: %v", err)
	}
	if data["author"] != "BlogAuthor" {
		t.Errorf("expected subdirectory 'author' to win, got %v", data["author"])
	}
}

// writeSitemap
func TestWriteSitemap_IndexHTMLStripped(t *testing.T) {
	root := t.TempDir()
	pages := []string{
		filepath.Join(root, "index.html"),
		filepath.Join(root, "about", "index.html"),
		filepath.Join(root, "contact.html"),
	}
	for _, p := range pages {
		writeFile(t, p, "")
	}

	if err := writeSitemap(root, pages, "https://example.com", true); err != nil {
		t.Fatalf("writeSitemap: %v", err)
	}

	raw := readFile(t, filepath.Join(root, "sitemap.xml"))
	if strings.Contains(raw, "index.html") {
		t.Error("sitemap should not contain 'index.html' in URLs")
	}
	assertContains(t, raw, "https://example.com")
}

// ssgList
func TestSsgList_PrintsPages(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")

	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `<p>home</p>`)
	writeFile(t, filepath.Join(srcDir, "pages", "about.html"), `<p>about</p>`)
	writeFile(t, filepath.Join(srcDir, "pages", "_layout.html"), `layout`)

	if err := ssgList(srcDir); err != nil {
		t.Fatalf("ssgList: %v", err)
	}
}

func TestSsgList_ErrorsOnMissingPagesDir(t *testing.T) {
	root := t.TempDir()
	if err := ssgList(root); err == nil {
		t.Error("expected error for missing pages dir, got nil")
	}
}

func TestSsgList_IncludesDataKeys(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")

	writeFile(t, filepath.Join(srcDir, "pages", "_data.json"), `{"site":"Test"}`)
	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `{{.site}}`)

	if err := ssgList(srcDir); err != nil {
		t.Fatalf("ssgList with data: %v", err)
	}
}
