package main

import (
	"encoding/xml"
	"path/filepath"
	"strings"
	"testing"
)

// correct output paths and content
func TestSsgBuild_BasicPage(t *testing.T) {
	root := t.TempDir()
	srcDir := minimalProject(t, root)
	buildDir := filepath.Join(root, "dist")

	if err := ssgBuild(BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}); err != nil {
		t.Fatalf("ssgBuild: %v", err)
	}

	assertFileExists(t, filepath.Join(buildDir, "index.html"))
	assertContains(t, readFile(t, filepath.Join(buildDir, "index.html")), "Hello")
}

func TestSsgBuild_UnderscorePagesAreSkipped(t *testing.T) {
	root := t.TempDir()
	srcDir := minimalProject(t, root)
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "_hidden.html"), `<p>hidden</p>`)

	if err := ssgBuild(BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}); err != nil {
		t.Fatalf("ssgBuild: %v", err)
	}

	assertFileNotExists(t, filepath.Join(buildDir, "_hidden.html"))
}

func TestSsgBuild_NonHTMLFilesAreCopied(t *testing.T) {
	root := t.TempDir()
	srcDir := minimalProject(t, root)
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "robots.txt"), "User-agent: *")

	if err := ssgBuild(BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}); err != nil {
		t.Fatalf("ssgBuild: %v", err)
	}

	assertFileExists(t, filepath.Join(buildDir, "robots.txt"))
}

// templates and data
func TestSsgBuild_TemplateDataMerge(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "_data.json"), `{"site":"MySite","author":"Global"}`)
	writeFile(t, filepath.Join(srcDir, "pages", "index.json"), `{"author":"Local"}`)
	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `{{.site}} by {{.author}}`)

	if err := ssgBuild(BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}); err != nil {
		t.Fatalf("ssgBuild: %v", err)
	}

	content := readFile(t, filepath.Join(buildDir, "index.html"))
	assertContains(t, content, "MySite")
	assertContains(t, content, "Local")
}

func TestSsgBuild_GlobalLayoutWrapsPage(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "_layout.html"),
		`<!DOCTYPE html><html><body>{{template "index.html" .}}</body></html>`)
	writeFile(t, filepath.Join(srcDir, "pages", "index.html"),
		`{{define "index.html"}}<p>content</p>{{end}}`)

	if err := ssgBuild(BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}); err != nil {
		t.Fatalf("ssgBuild: %v", err)
	}

	content := readFile(t, filepath.Join(buildDir, "index.html"))
	assertContains(t, content, "<!DOCTYPE html>")
	assertContains(t, content, "<p>content</p>")
}

func TestSsgBuild_TemplateError_NoPartialOutput(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "_layout.html"),
		`<!DOCTYPE html>{{template "content" .}}`)
	writeFile(t, filepath.Join(srcDir, "pages", "index.html"),
		`{{define "content"}}{{template "doesnotexist" .}}{{end}}`)

	if err := ssgBuild(BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}); err == nil {
		t.Fatal("expected build error, got nil")
	}

	// buffered rendering means no partial file should exist
	assertFileNotExists(t, filepath.Join(buildDir, "index.html"))
}

// assets
func TestSsgBuild_AssetManifestAndHashing(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "assets", "style.css"), "body { color: red; }")
	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `<link href="{{asset "style.css"}}">`)

	if err := ssgBuild(BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}); err != nil {
		t.Fatalf("ssgBuild: %v", err)
	}

	content := readFile(t, filepath.Join(buildDir, "index.html"))
	if strings.Contains(content, `href="/assets/style.css"`) {
		t.Error("expected hashed asset URL, got plain style.css")
	}
	assertContains(t, content, "/assets/style.")
}

// sitemap and robots.txt
func TestSsgBuild_SitemapGenerated(t *testing.T) {
	root := t.TempDir()
	srcDir := minimalProject(t, root)
	buildDir := filepath.Join(root, "dist")

	if err := ssgBuild(BuildFlags{SrcDir: srcDir, BuildDir: buildDir, BaseURL: "https://example.com", Quiet: true}); err != nil {
		t.Fatalf("ssgBuild: %v", err)
	}

	assertFileExists(t, filepath.Join(buildDir, "sitemap.xml"))

	type urlset struct {
		XMLName xml.Name `xml:"urlset"`
		URLs    []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	var s urlset
	if err := xml.Unmarshal([]byte(readFile(t, filepath.Join(buildDir, "sitemap.xml"))), &s); err != nil {
		t.Fatalf("invalid sitemap XML: %v", err)
	}
	if len(s.URLs) == 0 {
		t.Error("sitemap has no URLs")
	}
	if !strings.HasPrefix(s.URLs[0].Loc, "https://example.com") {
		t.Errorf("sitemap URL missing base: %s", s.URLs[0].Loc)
	}
}

func TestSsgBuild_RobotsTxtGenerated(t *testing.T) {
	root := t.TempDir()
	srcDir := minimalProject(t, root)
	buildDir := filepath.Join(root, "dist")

	if err := ssgBuild(BuildFlags{SrcDir: srcDir, BuildDir: buildDir, BaseURL: "https://example.com", Quiet: true}); err != nil {
		t.Fatalf("ssgBuild: %v", err)
	}

	content := readFile(t, filepath.Join(buildDir, "robots.txt"))
	assertContains(t, content, "User-agent: *")
	assertContains(t, content, "https://example.com/sitemap.xml")
}

func TestSsgBuild_IncrementalSitemapContainsAllPages(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `<p>home</p>`)
	writeFile(t, filepath.Join(srcDir, "pages", "about.html"), `<p>about</p>`)

	flags := BuildFlags{SrcDir: srcDir, BuildDir: buildDir, BaseURL: "https://example.com", Quiet: true}

	if err := ssgBuild(flags); err != nil {
		t.Fatalf("first build: %v", err)
	}

	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `<p>home v2</p>`)

	if err := ssgBuild(flags); err != nil {
		t.Fatalf("second build: %v", err)
	}

	type urlset struct {
		XMLName xml.Name `xml:"urlset"`
		URLs    []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	var s urlset
	if err := xml.Unmarshal([]byte(readFile(t, filepath.Join(buildDir, "sitemap.xml"))), &s); err != nil {
		t.Fatalf("invalid sitemap XML after incremental build: %v", err)
	}

	if len(s.URLs) != 2 {
		t.Errorf("expected 2 URLs in sitemap after incremental build, got %d", len(s.URLs))
	}

	locs := make(map[string]bool, len(s.URLs))
	for _, u := range s.URLs {
		locs[u.Loc] = true
	}
	if !locs["https://example.com/"] && !locs["https://example.com/index.html"] {
		t.Error("sitemap missing index page after incremental build")
	}
	if !locs["https://example.com/about.html"] {
		t.Error("sitemap missing about page after incremental build")
	}
}

func TestSsgBuild_ExistingRobotsTxtNotOverwritten(t *testing.T) {
	root := t.TempDir()
	srcDir := minimalProject(t, root)
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "robots.txt"), "User-agent: Googlebot\nDisallow: /secret/")

	if err := ssgBuild(BuildFlags{SrcDir: srcDir, BuildDir: buildDir, BaseURL: "https://example.com", Quiet: true}); err != nil {
		t.Fatalf("ssgBuild: %v", err)
	}

	assertContains(t, readFile(t, filepath.Join(buildDir, "robots.txt")), "Googlebot")
}

// flags
func TestSsgBuild_Verbose_PrintsDataKeys(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "_data.json"), `{"site":"Test","author":"Me"}`)
	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `{{.site}}`)

	if err := ssgBuild(BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Verbose: true}); err != nil {
		t.Fatalf("ssgBuild with verbose: %v", err)
	}
}

// error paths
func TestSsgBuild_ErrorsWhenPagesDirMissing(t *testing.T) {
	root := t.TempDir()
	if err := ssgBuild(BuildFlags{SrcDir: root, BuildDir: filepath.Join(root, "dist"), Quiet: true}); err == nil {
		t.Error("expected error for missing pages dir, got nil")
	}
}

func TestSsgBuild_TempDirCleanedOnFailure(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `{{.Unclosed`)

	if err := ssgBuild(BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}); err == nil {
		t.Fatal("expected build error for bad template, got nil")
	}

	entries, _ := filepath.Glob(filepath.Join(root, ".ssg-build-tmp-*"))
	if len(entries) > 0 {
		t.Errorf("temp directory not cleaned up: %v", entries)
	}
}

func TestSsgBuild_ValidateAssets_MissingAssetErrors(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `<link href="{{asset "missing.css"}}">`)

	if err := ssgBuild(BuildFlags{SrcDir: srcDir, BuildDir: buildDir, ValidateAssets: true, Quiet: true}); err == nil {
		t.Error("expected error for missing asset, got nil")
	}
}

func TestSsgBuild_CheckLinks_BrokenLinkErrors(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `<a href="/does-not-exist.html">broken</a>`)

	if err := ssgBuild(BuildFlags{SrcDir: srcDir, BuildDir: buildDir, CheckLinks: true, Quiet: true}); err == nil {
		t.Error("expected error for broken internal link, got nil")
	}
}

func TestSsgBuild_CheckLinks_ValidLinksPass(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `<a href="/about.html">about</a>`)
	writeFile(t, filepath.Join(srcDir, "pages", "about.html"), `<p>about</p>`)

	if err := ssgBuild(BuildFlags{SrcDir: srcDir, BuildDir: buildDir, CheckLinks: true, Quiet: true}); err != nil {
		t.Fatalf("expected no error for valid links, got: %v", err)
	}
}
