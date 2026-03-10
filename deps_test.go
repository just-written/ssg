package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// depGraph.record and depGraph.changed
func TestDepGraph_ChangedWhenOutputMissing(t *testing.T) {
	root := t.TempDir()
	g := newDepGraph()

	srcFile := filepath.Join(root, "index.html")
	writeFile(t, srcFile, "<p>hello</p>")
	g.record("index.html", []string{srcFile})

	changed, err := g.changed("index.html", filepath.Join(root, "dist", "index.html"))
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when output file is missing")
	}
}

func TestDepGraph_NotChangedWhenDepsUnmodified(t *testing.T) {
	root := t.TempDir()
	g := newDepGraph()

	srcFile := filepath.Join(root, "index.html")
	writeFile(t, srcFile, "<p>hello</p>")
	outFile := filepath.Join(root, "dist", "index.html")
	writeFile(t, outFile, "<p>hello</p>")
	g.record("index.html", []string{srcFile})

	changed, err := g.changed("index.html", outFile)
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if changed {
		t.Error("expected changed=false when deps are unmodified")
	}
}

func TestDepGraph_ChangedWhenDepModified(t *testing.T) {
	root := t.TempDir()
	g := newDepGraph()

	srcFile := filepath.Join(root, "index.html")
	writeFile(t, srcFile, "<p>v1</p>")
	outFile := filepath.Join(root, "dist", "index.html")
	writeFile(t, outFile, "<p>v1</p>")
	g.record("index.html", []string{srcFile})

	writeFile(t, srcFile, "<p>v2</p>")

	changed, err := g.changed("index.html", outFile)
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if !changed {
		t.Error("expected changed=true after dep was modified")
	}
}

func TestDepGraph_ChangedWhenDepDeleted(t *testing.T) {
	root := t.TempDir()
	g := newDepGraph()

	srcFile := filepath.Join(root, "layout.html")
	writeFile(t, srcFile, "<html></html>")
	outFile := filepath.Join(root, "dist", "index.html")
	writeFile(t, outFile, "<html></html>")
	g.record("index.html", []string{srcFile})

	os.Remove(srcFile)

	changed, err := g.changed("index.html", outFile)
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when a dep file is deleted")
	}
}

func TestDepGraph_ChangedWhenNoRecord(t *testing.T) {
	root := t.TempDir()
	g := newDepGraph()
	outFile := filepath.Join(root, "dist", "index.html")
	writeFile(t, outFile, "<p>hello</p>")

	changed, err := g.changed("index.html", outFile)
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when page has no dep record")
	}
}

// save and load
func TestDepGraph_SaveAndLoad_RoundTrip(t *testing.T) {
	root := t.TempDir()
	srcFile := filepath.Join(root, "src", "index.html")
	writeFile(t, srcFile, "<p>hello</p>")

	g := newDepGraph()
	g.record("index.html", []string{srcFile})
	if err := g.save(root); err != nil {
		t.Fatalf("save: %v", err)
	}

	g2, err := loadDepGraph(root)
	if err != nil {
		t.Fatalf("loadDepGraph: %v", err)
	}
	if _, ok := g2.Pages["index.html"]; !ok {
		t.Error("expected index.html entry to survive round-trip")
	}
}

func TestDepGraph_LoadMissingFile_ReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	g, err := loadDepGraph(root)
	if err != nil {
		t.Fatalf("loadDepGraph on missing file: %v", err)
	}
	if len(g.Pages) != 0 {
		t.Errorf("expected empty graph, got %d entries", len(g.Pages))
	}
}

func TestDepGraph_LoadCorruptFile_ReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, graphFile), "this is not json {{{{")

	g, err := loadDepGraph(root)
	if err != nil {
		t.Fatalf("expected corrupt graph to be silently reset, got error: %v", err)
	}
	if len(g.Pages) != 0 {
		t.Errorf("expected empty graph after corrupt file, got %d entries", len(g.Pages))
	}
}

// stalePages
func TestDepGraph_StalePages_ReturnsAllWhenNoPriorGraph(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), "<p>home</p>")
	writeFile(t, filepath.Join(srcDir, "pages", "about.html"), "<p>about</p>")

	g := newDepGraph()
	stale, err := g.stalePages(filepath.Join(srcDir, "pages"), buildDir)
	if err != nil {
		t.Fatalf("stalePages: %v", err)
	}
	if len(stale) != 2 {
		t.Errorf("expected 2 stale pages with empty graph, got %d", len(stale))
	}
}

func TestDepGraph_StalePages_SkipsUnchangedPages(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	buildDir := filepath.Join(root, "dist")

	srcIndex := filepath.Join(pagesDir, "index.html")
	srcAbout := filepath.Join(pagesDir, "about.html")
	writeFile(t, srcIndex, "<p>home</p>")
	writeFile(t, srcAbout, "<p>about</p>")
	writeFile(t, filepath.Join(buildDir, "index.html"), "<p>home</p>")
	writeFile(t, filepath.Join(buildDir, "about.html"), "<p>about</p>")

	g := newDepGraph()
	g.record("index.html", []string{srcIndex})
	g.record("about.html", []string{srcAbout})

	stale, err := g.stalePages(pagesDir, buildDir)
	if err != nil {
		t.Fatalf("stalePages: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("expected 0 stale pages when nothing changed, got %d", len(stale))
	}
}

func TestDepGraph_StalePages_DetectsModifiedDep(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	buildDir := filepath.Join(root, "dist")

	layout := filepath.Join(pagesDir, "_layout.html")
	srcIndex := filepath.Join(pagesDir, "index.html")
	writeFile(t, layout, "<html>v1</html>")
	writeFile(t, srcIndex, "<p>home</p>")
	writeFile(t, filepath.Join(buildDir, "index.html"), "<p>home</p>")

	g := newDepGraph()
	g.record("index.html", []string{srcIndex, layout})

	writeFile(t, layout, "<html>v2</html>")

	stale, err := g.stalePages(pagesDir, buildDir)
	if err != nil {
		t.Fatalf("stalePages: %v", err)
	}
	if len(stale) != 1 {
		t.Errorf("expected 1 stale page after layout change, got %d", len(stale))
	}
}

func TestDepGraph_StalePages_SkipsUnderscoreFiles(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(pagesDir, "_layout.html"), "<html></html>")
	writeFile(t, filepath.Join(pagesDir, "index.html"), "<p>home</p>")
	writeFile(t, filepath.Join(buildDir, "index.html"), "<p>home</p>")

	g := newDepGraph()
	g.record("index.html", []string{filepath.Join(pagesDir, "index.html")})

	stale, err := g.stalePages(pagesDir, buildDir)
	if err != nil {
		t.Fatalf("stalePages: %v", err)
	}
	for _, sp := range stale {
		if filepath.Base(sp.srcPath) == "_layout.html" {
			t.Error("_layout.html should not appear as a stale page")
		}
	}
}

// incremental build integration
func TestSsgBuild_IncrementalSkipsUnchangedPages(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `<p>home</p>`)
	writeFile(t, filepath.Join(srcDir, "pages", "about.html"), `<p>about</p>`)

	flags := BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}

	if err := ssgBuild(flags); err != nil {
		t.Fatalf("first build: %v", err)
	}

	aboutOut := filepath.Join(buildDir, "about.html")
	info1, err := os.Stat(aboutOut)
	if err != nil {
		t.Fatalf("stat about.html: %v", err)
	}

	if err := ssgBuild(flags); err != nil {
		t.Fatalf("second build: %v", err)
	}

	info2, err := os.Stat(aboutOut)
	if err != nil {
		t.Fatalf("stat about.html after second build: %v", err)
	}
	if !info2.ModTime().Equal(info1.ModTime()) {
		t.Error("expected about.html to be untouched on second build with no changes")
	}
}

func TestSsgBuild_IncrementalRebuildsChangedPage(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `<p>v1</p>`)
	flags := BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}

	if err := ssgBuild(flags); err != nil {
		t.Fatalf("first build: %v", err)
	}

	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `<p>v2</p>`)

	if err := ssgBuild(flags); err != nil {
		t.Fatalf("second build: %v", err)
	}

	assertContains(t, readFile(t, filepath.Join(buildDir, "index.html")), "v2")
}

func TestSsgBuild_IncrementalRebuildsWhenSharedLayoutChanges(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "_layout.html"),
		`<!DOCTYPE html><html><body>{{template "index.html" .}}</body></html>`)
	writeFile(t, filepath.Join(srcDir, "pages", "index.html"),
		`{{define "index.html"}}<p>content</p>{{end}}`)

	flags := BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}

	if err := ssgBuild(flags); err != nil {
		t.Fatalf("first build: %v", err)
	}

	writeFile(t, filepath.Join(srcDir, "pages", "_layout.html"),
		`<!DOCTYPE html><html><body class="new">{{template "index.html" .}}</body></html>`)

	if err := ssgBuild(flags); err != nil {
		t.Fatalf("second build: %v", err)
	}

	assertContains(t, readFile(t, filepath.Join(buildDir, "index.html")), `class="new"`)
}

func TestSsgBuild_ForceFlag_RebuildsEverything(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `<p>hello</p>`)
	flags := BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}

	if err := ssgBuild(flags); err != nil {
		t.Fatalf("first build: %v", err)
	}

	outFile := filepath.Join(buildDir, "index.html")
	info1, _ := os.Stat(outFile)

	flags.Force = true
	if err := ssgBuild(flags); err != nil {
		t.Fatalf("forced rebuild: %v", err)
	}

	info2, _ := os.Stat(outFile)
	if info2.ModTime().Equal(info1.ModTime()) {
		t.Error("expected forced rebuild to produce a newer output file")
	}
}

func TestSsgBuild_DepGraphSavedAfterBuild(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `<p>hello</p>`)

	if err := ssgBuild(BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}); err != nil {
		t.Fatalf("build: %v", err)
	}

	assertFileExists(t, filepath.Join(srcDir, graphFile))
	assertFileNotExists(t, filepath.Join(buildDir, graphFile))
}

// pruneOutputs
func TestDepGraph_PruneOutputs_RemovesStaleFile(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(buildDir, "index.html"), "<p>home</p>")
	writeFile(t, filepath.Join(buildDir, "old-page.html"), "<p>gone</p>")

	g := newDepGraph()
	g.recordOutput("index.html")

	if err := g.pruneOutputs(buildDir, true); err != nil {
		t.Fatalf("pruneOutputs: %v", err)
	}

	assertFileExists(t, filepath.Join(buildDir, "index.html"))
	assertFileNotExists(t, filepath.Join(buildDir, "old-page.html"))
}

func TestDepGraph_PruneOutputs_RemovesEmptyDirs(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(buildDir, "blog", "old-post.html"), "<p>old</p>")
	writeFile(t, filepath.Join(buildDir, "index.html"), "<p>home</p>")

	g := newDepGraph()
	g.recordOutput("index.html")

	if err := g.pruneOutputs(buildDir, true); err != nil {
		t.Fatalf("pruneOutputs: %v", err)
	}

	assertFileNotExists(t, filepath.Join(buildDir, "blog", "old-post.html"))
	if _, err := os.Stat(filepath.Join(buildDir, "blog")); err == nil {
		t.Error("expected empty blog/ directory to be removed")
	}
}

// syncAssets
func TestSyncAssets_CopiesNewAsset(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "assets", "style.css"), "body{}")

	manifest, changed, err := syncAssets(srcDir, buildDir, map[string]string{}, true)
	if err != nil {
		t.Fatalf("syncAssets: %v", err)
	}
	if !changed {
		t.Error("expected changed=true for new asset")
	}
	if _, ok := manifest["style.css"]; !ok {
		t.Error("expected style.css in manifest")
	}
}

func TestSyncAssets_NoChangeWhenAssetUnmodified(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "assets", "style.css"), "body{}")

	manifest, _, err := syncAssets(srcDir, buildDir, map[string]string{}, true)
	if err != nil {
		t.Fatalf("first syncAssets: %v", err)
	}

	_, changed, err := syncAssets(srcDir, buildDir, manifest, true)
	if err != nil {
		t.Fatalf("second syncAssets: %v", err)
	}
	if changed {
		t.Error("expected changed=false when asset is unmodified")
	}
}

func TestSyncAssets_DetectsRemovedAsset(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	prevManifest := map[string]string{"old.css": "/assets/old.abc123.css"}

	_, changed, err := syncAssets(srcDir, buildDir, prevManifest, true)
	if err != nil {
		t.Fatalf("syncAssets: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when a source asset was removed")
	}
}

// incremental build: deleted page pruning
func TestSsgBuild_IncrementalPrunesDeletedPage(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `<p>home</p>`)
	writeFile(t, filepath.Join(srcDir, "pages", "about.html"), `<p>about</p>`)

	flags := BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}

	if err := ssgBuild(flags); err != nil {
		t.Fatalf("first build: %v", err)
	}
	assertFileExists(t, filepath.Join(buildDir, "about.html"))

	os.Remove(filepath.Join(srcDir, "pages", "about.html"))

	if err := ssgBuild(flags); err != nil {
		t.Fatalf("second build: %v", err)
	}

	assertFileNotExists(t, filepath.Join(buildDir, "about.html"))
}

func TestSsgBuild_IncrementalPrunesRemovedAsset(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "assets", "style.css"), "body{}")
	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `<link href="{{asset "style.css"}}">`)

	flags := BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}

	if err := ssgBuild(flags); err != nil {
		t.Fatalf("first build: %v", err)
	}

	var assetFile string
	filepath.WalkDir(filepath.Join(buildDir, "assets"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			assetFile = path
		}
		return err
	})
	if assetFile == "" {
		t.Fatal("no asset file found after first build")
	}

	os.Remove(filepath.Join(srcDir, "assets", "style.css"))
	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `<p>no more stylesheet</p>`)

	if err := ssgBuild(flags); err != nil {
		t.Fatalf("second build: %v", err)
	}

	assertFileNotExists(t, assetFile)
}
