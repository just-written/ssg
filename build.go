// full builds via atomic temp-dir swap
// incremental builds via file hash dep graph
// all garbage, all very poorly tested
package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template/parse"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/sync/errgroup"
)

type BuildFlags struct {
	SrcDir         string
	BuildDir       string
	BaseURL        string
	ValidateAssets bool
	CheckLinks     bool
	Verbose        bool
	Quiet          bool
	Force          bool
}

func ssgBuild(flags BuildFlags) error {
	pagesDir := filepath.Join(flags.SrcDir, "pages")
	if _, err := os.Stat(pagesDir); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("pages directory not found: %s", pagesDir)
	}

	if !flags.Force {
		if _, err := os.Stat(flags.BuildDir); err == nil {
			return ssgBuildIncremental(flags, pagesDir)
		}
	}
	return ssgBuildFull(flags, pagesDir)
}

func ssgBuildFull(flags BuildFlags, pagesDir string) error {
	tmpDir, err := os.MkdirTemp(filepath.Dir(flags.BuildDir), ".ssg-build-tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp build directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	start := time.Now()
	graph := newDepGraph()

	assetManifest, assetsCount, err := copyAssets(flags.SrcDir, tmpDir, flags.Quiet)
	if err != nil {
		return fmt.Errorf("copying assets: %w", err)
	}
	for _, outputURL := range assetManifest {
		graph.recordOutput(strings.TrimPrefix(outputURL, "/"))
	}
	graph.Assets = assetManifest

	pages, filesCopied, err := walkPages(pagesDir, tmpDir, flags.Quiet, graph)
	if err != nil {
		return err
	}

	var mu sync.Mutex
	var builtPages []string
	eg := new(errgroup.Group)
	for _, pagePath := range pages {
		eg.Go(func() error {
			outPath, deps, err := buildPage(pagesDir, tmpDir, pagePath, assetManifest, flags)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(tmpDir, outPath)
			mu.Lock()
			builtPages = append(builtPages, outPath)
			graph.record(filepath.ToSlash(rel), deps)
			graph.recordOutput(rel)
			mu.Unlock()
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	if flags.BaseURL != "" {
		if err := writeSitemap(tmpDir, builtPages, flags.BaseURL, flags.Quiet); err != nil {
			return fmt.Errorf("writing sitemap: %w", err)
		}
		graph.recordOutput("sitemap.xml")
	}
	if err := writeRobotsTxtOutput(tmpDir, graph, flags); err != nil {
		return err
	}

	if flags.CheckLinks {
		if n, err := checkBrokenLinks(tmpDir); err != nil {
			return fmt.Errorf("checking links: %w", err)
		} else if n > 0 {
			return fmt.Errorf("%d broken internal link(s) found", n)
		}
	}

	if err := os.RemoveAll(flags.BuildDir); err != nil {
		return fmt.Errorf("clearing build directory: %w", err)
	}
	if err := os.Rename(tmpDir, flags.BuildDir); err != nil {
		if err := copyDir(tmpDir, flags.BuildDir); err != nil {
			return fmt.Errorf("replacing build directory: %w", err)
		}
		os.RemoveAll(tmpDir)
	}

	if err := graph.save(flags.SrcDir); err != nil {
		fmt.Printf("warning: could not save dep graph: %v\n", err)
	}

	if !flags.Quiet {
		fmt.Printf("\nbuilt %d page(s), copied %d file(s) and %d asset(s) in %s\n",
			len(builtPages), filesCopied, assetsCount, time.Since(start).Round(time.Millisecond))
	}
	return nil
}

func ssgBuildIncremental(flags BuildFlags, pagesDir string) error {
	start := time.Now()

	prevGraph, err := loadDepGraph(flags.SrcDir)
	if err != nil {
		if !flags.Quiet {
			fmt.Printf("dep graph unreadable (%v), falling back to full build\n", err)
		}
		return ssgBuildFull(flags, pagesDir)
	}

	graph := newDepGraph()
	maps.Copy(graph.Pages, prevGraph.Pages)

	assetManifest, assetsChanged, err := syncAssets(flags.SrcDir, flags.BuildDir, prevGraph.Assets, flags.Quiet)
	if err != nil {
		return fmt.Errorf("syncing assets: %w", err)
	}
	graph.Assets = assetManifest
	for _, outputURL := range assetManifest {
		graph.recordOutput(strings.TrimPrefix(outputURL, "/"))
	}

	filesCopied := 0
	err = filepath.WalkDir(pagesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if strings.HasPrefix(name, "_") || strings.HasSuffix(name, ".html") {
			return nil
		}
		outPath, err := resolveOutputPath(pagesDir, flags.BuildDir, path)
		if err != nil {
			return err
		}
		if !isWithinDir(flags.BuildDir, outPath) {
			return fmt.Errorf("path traversal detected: %s", path)
		}
		rel, _ := filepath.Rel(flags.BuildDir, outPath)
		graph.recordOutput(rel)

		srcInfo, err := os.Stat(path)
		if err != nil {
			return err
		}
		dstInfo, err := os.Stat(outPath)
		if errors.Is(err, os.ErrNotExist) || (err == nil && srcInfo.ModTime().After(dstInfo.ModTime())) {
			if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
				return err
			}
			if err := copyFile(path, outPath); err != nil {
				return fmt.Errorf("copying %s: %w", path, err)
			}
			if !flags.Quiet {
				fmt.Printf("copied: %s\n", rel)
			}
			filesCopied++
		}
		return nil
	})
	if err != nil {
		return err
	}

	stale, err := prevGraph.stalePages(pagesDir, flags.BuildDir)
	if err != nil {
		return fmt.Errorf("computing stale pages: %w", err)
	}
	if assetsChanged {
		if !flags.Quiet && len(stale) > 0 {
			fmt.Println("asset URLs changed, rebuilding all pages")
		}
		stale, err = prevGraph.allPages(pagesDir, flags.BuildDir)
		if err != nil {
			return fmt.Errorf("listing all pages: %w", err)
		}
	}

	var mu sync.Mutex
	var builtPages []string
	eg := new(errgroup.Group)
	for _, sp := range stale {
		eg.Go(func() error {
			outPath, deps, err := buildPage(pagesDir, flags.BuildDir, sp.srcPath, assetManifest, flags)
			if err != nil {
				return err
			}
			mu.Lock()
			builtPages = append(builtPages, outPath)
			graph.record(sp.outputRel, deps)
			mu.Unlock()
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	err = filepath.WalkDir(pagesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if strings.HasPrefix(name, "_") || !strings.HasSuffix(name, ".html") {
			return nil
		}
		rel, err := filepath.Rel(pagesDir, path)
		if err != nil {
			return err
		}
		graph.recordOutput(rel)
		return nil
	})
	if err != nil {
		return err
	}

	if flags.BaseURL != "" {
		var sitemapPages []string
		for rel := range graph.KnownOutputs {
			if strings.HasSuffix(rel, ".html") {
				sitemapPages = append(sitemapPages, filepath.Join(flags.BuildDir, filepath.FromSlash(rel)))
			}
		}
		if err := writeSitemap(flags.BuildDir, sitemapPages, flags.BaseURL, flags.Quiet); err != nil {
			return fmt.Errorf("writing sitemap: %w", err)
		}
		graph.recordOutput("sitemap.xml")
	}
	if err := writeRobotsTxtOutput(flags.BuildDir, graph, flags); err != nil {
		return err
	}

	if flags.CheckLinks && len(builtPages) > 0 {
		if n, err := checkBrokenLinks(flags.BuildDir); err != nil {
			return fmt.Errorf("checking links: %w", err)
		} else if n > 0 {
			return fmt.Errorf("%d broken internal link(s) found", n)
		}
	}

	if err := graph.pruneOutputs(flags.BuildDir, flags.Quiet); err != nil {
		return fmt.Errorf("pruning stale outputs: %w", err)
	}

	if err := graph.save(flags.SrcDir); err != nil {
		fmt.Printf("warning: could not save dep graph: %v\n", err)
	}

	if len(builtPages) == 0 && filesCopied == 0 && !assetsChanged {
		if !flags.Quiet {
			fmt.Printf("\nnothing to do (up to date) in %s\n", time.Since(start).Round(time.Millisecond))
		}
		return nil
	}

	if !flags.Quiet {
		fmt.Printf("\nrebuilt %d page(s), copied %d file(s) and %d asset(s) in %s\n",
			len(builtPages), filesCopied, len(assetManifest), time.Since(start).Round(time.Millisecond))
	}
	return nil
}

func walkPages(pagesDir, outDir string, quiet bool, graph *depGraph) (pages []string, filesCopied int, err error) {
	err = filepath.WalkDir(pagesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if strings.HasPrefix(name, "_") {
			return nil
		}
		outPath, err := resolveOutputPath(pagesDir, outDir, path)
		if err != nil {
			return err
		}
		if !isWithinDir(outDir, outPath) {
			return fmt.Errorf("path traversal detected: %s resolves outside build directory", path)
		}
		if strings.HasSuffix(name, ".html") {
			pages = append(pages, path)
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return err
		}
		if err := copyFile(path, outPath); err != nil {
			return fmt.Errorf("copying %s: %w", path, err)
		}
		rel, _ := filepath.Rel(outDir, outPath)
		if !quiet {
			fmt.Printf("copied: %s\n", rel)
		}
		graph.recordOutput(rel)
		filesCopied++
		return nil
	})
	return pages, filesCopied, err
}

func writeRobotsTxtOutput(outDir string, graph *depGraph, flags BuildFlags) error {
	if err := writeRobotsTxt(flags.SrcDir, outDir, flags.BaseURL, flags.Quiet); err != nil {
		return fmt.Errorf("writing robots.txt: %w", err)
	}
	graph.recordOutput("robots.txt")
	return nil
}

func buildPage(pagesDir, buildDir, pagePath string, assetManifest map[string]string, flags BuildFlags) (string, []string, error) {
	pageDir := filepath.Dir(pagePath)

	tmpl, templateFiles, err := loadTemplates(pagesDir, pageDir, pagePath, assetManifest)
	if err != nil {
		return "", nil, fmt.Errorf("loading templates for %s: %w", pagePath, err)
	}

	if flags.ValidateAssets {
		if err := validateAssetRefs(tmpl, assetManifest); err != nil {
			return "", nil, fmt.Errorf("invalid asset reference in %s: %w", pagePath, err)
		}
	}

	data, dataFiles, err := loadPageData(pagesDir, pagePath)
	if err != nil {
		return "", nil, fmt.Errorf("loading data for %s: %w", pagePath, err)
	}

	outPath, err := resolveOutputPath(pagesDir, buildDir, pagePath)
	if err != nil {
		return "", nil, err
	}

	entry := filepath.Base(pagePath)
	if hasLayout(filepath.Join(pageDir, "_layout.html")) {
		entry = "_layout.html"
	} else if hasLayout(filepath.Join(pagesDir, "_layout.html")) {
		entry = "_layout.html"
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, entry, data); err != nil {
		rel, _ := filepath.Rel(pagesDir, pagePath)
		return "", nil, fmt.Errorf("template error in %s: %w", rel, err)
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return "", nil, fmt.Errorf("creating output directory for %s: %w", outPath, err)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return "", nil, fmt.Errorf("creating output file %s: %w", outPath, err)
	}
	defer f.Close()

	if _, err := buf.WriteTo(f); err != nil {
		return "", nil, fmt.Errorf("writing output file %s: %w", outPath, err)
	}
	if err := f.Sync(); err != nil {
		return "", nil, fmt.Errorf("flushing output file %s: %w", outPath, err)
	}

	deps := append(templateFiles, dataFiles...)
	rel, _ := filepath.Rel(buildDir, outPath)
	if !flags.Quiet {
		fmt.Printf("built: %s\n  loaded files: %s\n", rel, strings.Join(deps, ", "))
	}
	if flags.Verbose {
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		fmt.Printf("  data keys: %s\n", strings.Join(keys, ", "))
	}

	return outPath, deps, nil
}

func loadTemplates(pagesDir, pageDir, pagePath string, assetManifest map[string]string) (*template.Template, []string, error) {
	var files []string

	partialsDir := filepath.Join(filepath.Dir(pagesDir), "partials")
	entries, err := os.ReadDir(partialsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("reading partials directory %s: %w", partialsDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".html") {
			files = append(files, filepath.Join(partialsDir, e.Name()))
		}
	}

	hasLocalLayout := false
	localEntries, err := os.ReadDir(pageDir)
	if err != nil {
		return nil, nil, fmt.Errorf("reading page directory %s: %w", pageDir, err)
	}
	for _, e := range localEntries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") || !strings.HasPrefix(e.Name(), "_") {
			continue
		}
		files = append(files, filepath.Join(pageDir, e.Name()))
		if e.Name() == "_layout.html" && hasLayout(filepath.Join(pageDir, e.Name())) {
			hasLocalLayout = true
		}
	}

	if !hasLocalLayout && pageDir != pagesDir {
		if globalLayout := filepath.Join(pagesDir, "_layout.html"); hasLayout(globalLayout) {
			files = append(files, globalLayout)
		}
	}

	files = append(files, pagePath)

	funcMap := template.FuncMap{
		"asset": func(logicalPath string) (string, error) {
			if hashed, ok := assetManifest[filepath.ToSlash(logicalPath)]; ok {
				return hashed, nil
			}
			return "", fmt.Errorf("asset not found: %s", logicalPath)
		},
	}

	tmpl, err := template.New(filepath.Base(pagePath)).Funcs(funcMap).ParseFiles(files...)
	if err != nil {
		return nil, nil, err
	}
	return tmpl, files, nil
}

func validateAssetRefs(tmpl *template.Template, manifest map[string]string) error {
	var missing []string
	for _, t := range tmpl.Templates() {
		if t.Tree == nil {
			continue
		}
		walkNodes(t.Tree.Root, func(node parse.Node) {
			cmd, ok := node.(*parse.CommandNode)
			if !ok || len(cmd.Args) < 2 {
				return
			}
			ident, ok := cmd.Args[0].(*parse.IdentifierNode)
			if !ok || ident.Ident != "asset" {
				return
			}
			switch arg := cmd.Args[1].(type) {
			case *parse.StringNode:
				if _, ok := manifest[arg.Text]; !ok {
					missing = append(missing, fmt.Sprintf("  %q (in template %q)", arg.Text, t.Name()))
				}
			default:
				fmt.Printf("warning: dynamic asset reference in template %q cannot be validated at build time\n", t.Name())
			}
		})
	}
	if len(missing) > 0 {
		return fmt.Errorf("asset(s) not found:\n%s", strings.Join(missing, "\n"))
	}
	return nil
}

func walkNodes(node parse.Node, fn func(parse.Node)) {
	if node == nil {
		return
	}
	fn(node)
	switch n := node.(type) {
	case *parse.ListNode:
		for _, child := range n.Nodes {
			walkNodes(child, fn)
		}
	case *parse.IfNode:
		walkNodes(n.List, fn)
		walkNodes(n.ElseList, fn)
	case *parse.RangeNode:
		walkNodes(n.List, fn)
		walkNodes(n.ElseList, fn)
	case *parse.WithNode:
		walkNodes(n.List, fn)
		walkNodes(n.ElseList, fn)
	case *parse.ActionNode:
		walkNodes(n.Pipe, fn)
	case *parse.PipeNode:
		for _, cmd := range n.Cmds {
			walkNodes(cmd, fn)
		}
	case *parse.CommandNode:
		for _, arg := range n.Args {
			walkNodes(arg, fn)
		}
	}
}

func loadPageData(pagesDir, pagePath string) (map[string]any, []string, error) {
	merged := map[string]any{}
	var dataFiles []string

	dataPaths := []string{filepath.Join(pagesDir, "_data.json")}

	pageDir := filepath.Dir(pagePath)
	if pageDir != pagesDir {
		rel, err := filepath.Rel(pagesDir, pageDir)
		if err != nil {
			return nil, nil, err
		}
		current := pagesDir
		for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			dataPaths = append(dataPaths, filepath.Join(current, "_data.json"))
		}
	}
	dataPaths = append(dataPaths, strings.TrimSuffix(pagePath, ".html")+".json")

	for _, path := range dataPaths {
		data, err := readJSONFileOptional(path)
		if err != nil {
			return nil, nil, err
		}
		if data != nil {
			maps.Copy(merged, data)
			dataFiles = append(dataFiles, path)
		}
	}

	if len(merged) == 0 {
		return nil, dataFiles, nil
	}
	return merged, dataFiles, nil
}

func processAssets(srcDir, outDir string, prevManifest map[string]string, quiet bool) (manifest map[string]string, n int, err error) {
	assetsDir := filepath.Join(srcDir, "assets")
	if _, err := os.Stat(assetsDir); errors.Is(err, os.ErrNotExist) {
		if len(prevManifest) > 0 {
			return map[string]string{}, 1, nil 
		}
		return map[string]string{}, 0, nil
	}

	manifest = map[string]string{}
	err = filepath.WalkDir(assetsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		hash, err := hashFile(path)
		if err != nil {
			return fmt.Errorf("hashing %s: %w", path, err)
		}
		ext := filepath.Ext(d.Name())
		hashedName := strings.TrimSuffix(d.Name(), ext) + "." + hash + ext

		relDir, err := filepath.Rel(assetsDir, filepath.Dir(path))
		if err != nil {
			return err
		}
		logicalKey := strings.TrimPrefix(filepath.ToSlash(filepath.Join(relDir, d.Name())), "./")
		outputURL := "/" + filepath.ToSlash(filepath.Clean(filepath.Join("assets", relDir, hashedName)))
		manifest[logicalKey] = outputURL

		outPath := filepath.Join(outDir, filepath.FromSlash(strings.TrimPrefix(outputURL, "/")))
		if !isWithinDir(outDir, outPath) {
			return fmt.Errorf("path traversal detected: %s resolves outside build directory", path)
		}

		if prevManifest != nil {
			if _, err := os.Stat(outPath); err == nil {
				return nil
			}
		}

		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return err
		}
		if err := copyFile(path, outPath); err != nil {
			return fmt.Errorf("copying asset %s: %w", path, err)
		}
		if !quiet {
			fmt.Printf("asset:  %s -> %s\n", logicalKey, outputURL)
		}
		n++
		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	if prevManifest != nil {
		if assetsManifestDiffers(prevManifest, manifest) {
			return manifest, 1, nil
		}
		return manifest, 0, nil
	}
	return manifest, n, nil
}

func assetsManifestDiffers(prev, current map[string]string) bool {
	if len(prev) != len(current) {
		return true
	}
	for k, v := range current {
		if prev[k] != v {
			return true
		}
	}
	return false
}

func copyAssets(srcDir, outDir string, quiet bool) (map[string]string, int, error) {
	return processAssets(srcDir, outDir, nil, quiet)
}

func syncAssets(srcDir, buildDir string, prevManifest map[string]string, quiet bool) (map[string]string, bool, error) {
	manifest, n, err := processAssets(srcDir, buildDir, prevManifest, quiet)
	return manifest, n > 0, err
}

func writeSitemap(outDir string, pagePaths []string, baseURL string, quiet bool) error {
	type urlEntry struct {
		Loc string `xml:"loc"`
	}
	type urlset struct {
		XMLName xml.Name   `xml:"urlset"`
		Xmlns   string     `xml:"xmlns,attr"`
		URLs    []urlEntry `xml:"url"`
	}

	set := urlset{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	for _, p := range pagePaths {
		rel, err := filepath.Rel(outDir, p)
		if err != nil {
			return err
		}
		urlPath := "/" + filepath.ToSlash(rel)
		urlPath = strings.TrimSuffix(urlPath, "index.html")
		set.URLs = append(set.URLs, urlEntry{Loc: baseURL + urlPath})
	}

	out, err := os.Create(filepath.Join(outDir, "sitemap.xml"))
	if err != nil {
		return fmt.Errorf("creating sitemap.xml: %w", err)
	}
	defer out.Close()

	if _, err := out.WriteString(xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(out)
	enc.Indent("", "  ")
	if err := enc.Encode(set); err != nil {
		return fmt.Errorf("encoding sitemap: %w", err)
	}
	if !quiet {
		fmt.Println("sitemap: sitemap.xml")
	}
	return out.Sync()
}

func writeRobotsTxt(srcDir, outDir string, baseURL string, quiet bool) error {
	if _, err := os.Stat(filepath.Join(srcDir, "pages", "robots.txt")); err == nil {
		return nil
	}

	content := "User-agent: *\nAllow: /\n"
	if baseURL != "" {
		content += fmt.Sprintf("\nSitemap: %s/sitemap.xml\n", baseURL)
	}
	if err := os.WriteFile(filepath.Join(outDir, "robots.txt"), []byte(content), 0644); err != nil {
		return err
	}
	if !quiet {
		fmt.Println("robots:  robots.txt")
	}
	return nil
}

func checkBrokenLinks(buildDir string) (int, error) {
	var brokenCount int

	err := filepath.WalkDir(buildDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".html") {
			return err
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		doc, parseErr := html.Parse(f)
		f.Close()
		if parseErr != nil {
			return fmt.Errorf("parsing %s: %w", path, parseErr)
		}

		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode {
				for _, attr := range n.Attr {
					if attr.Key != "href" && attr.Key != "src" && attr.Key != "action" {
						continue
					}
					if broken, target := isBrokenLink(attr.Val, path, buildDir); broken {
						rel, _ := filepath.Rel(buildDir, path)
						fmt.Printf("warning: broken link %q in %s (resolved: %s)\n", attr.Val, rel, target)
						brokenCount++
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(doc)
		return nil
	})

	return brokenCount, err
}

func isBrokenLink(link, fromFile, buildDir string) (broken bool, target string) {
	if link == "" || strings.HasPrefix(link, "#") || strings.HasPrefix(link, "//") {
		return false, ""
	}

	if i := strings.IndexAny(link, ":/?#"); i >= 0 && link[i] == ':' {
		return false, ""
	}

	if before, _, found := strings.Cut(link, "?"); found {
		link = before
	}

	if before, _, found := strings.Cut(link, "#"); found {
		link = before
	}

	if link == "" {
		return false, ""
	}

	if strings.HasPrefix(link, "/") {
		target = filepath.Join(buildDir, filepath.FromSlash(link))
	} else {
		target = filepath.Join(filepath.Dir(fromFile), filepath.FromSlash(link))
	}

	if _, err := os.Stat(target); err == nil {
		return false, target
	}
	if _, err := os.Stat(filepath.Join(target, "index.html")); err == nil {
		return false, target
	}
	return true, target
}
