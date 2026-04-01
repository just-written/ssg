package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	texttmpl "text/template"
	ttparse "text/template/parse"
	"time"

	"github.com/spf13/cobra"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"golang.org/x/sync/errgroup"

	"ssg/embedded"
)

var gmRenderer = goldmark.New(
	goldmark.WithExtensions(
		extension.Table,
		extension.Strikethrough,
		extension.TaskList,
		extension.Footnote,
		extension.Linkify,
	),
)

var buildCmd = &cobra.Command{
	Use:   "build [flags]",
	Short: "Build the project",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := initConfig(cmd, "build")
		if err != nil {
			return err
		}
		return buildFunc(BuildConfig{
			In:       v.GetString("in"),
			Out:      v.GetString("out"),
			Pages:    v.GetString("pages"),
			Static:   v.GetString("static"),
			Partials: v.GetString("partials"),
			BaseURL:  v.GetString("base-url"),
		})
	},
}

type BuildConfig struct {
	In, Out, Pages, Static, Partials, BaseURL string
}

func init() {
	rootCmd.AddCommand(buildCmd)
	buildCmd.Flags().StringP("in", "i", ".", "Input project directory")
	buildCmd.Flags().StringP("out", "o", "dist/", "Output directory for built files")
	buildCmd.Flags().String("pages", "pages/", "Directory of pages to build")
	buildCmd.Flags().String("static", "static/", "Directory of static assets to copy")
	buildCmd.Flags().String("partials", "partials/", "Directory of partial templates")
	buildCmd.Flags().String("base-url", "", "Base URL for sitemap generation (e.g. https://example.com)")
}

func buildFunc(cfg BuildConfig) error {
	abs := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(cfg.In, p)
	}

	cfg.Pages, cfg.Static, cfg.Partials = abs(cfg.Pages), abs(cfg.Static), abs(cfg.Partials)

	parent := filepath.Dir(filepath.Clean(cfg.Out))
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("creating parent of output directory %s: %w", parent, err)
	}

	tmpDir, err := os.MkdirTemp(parent, ".build-tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp build directory: %w", err)
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(tmpDir)
		}
	}()

	staticManifest, err := copyStaticAssets(cfg.Static, tmpDir)
	if err != nil {
		return fmt.Errorf("copying static assets: %w", err)
	}

	partials, err := loadPartials(cfg.Partials)
	if err != nil {
		return fmt.Errorf("loading partials: %w", err)
	}

	var pages []string
	err = filepath.Walk(cfg.Pages, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".html" {
			return err
		}
		rel, err := filepath.Rel(cfg.Pages, path)
		if err != nil {
			return fmt.Errorf("computing relative path for %s: %w", path, err)
		}
		pages = append(pages, rel)
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking pages directory: %w", err)
	}

	if err := checkCycles(pages, cfg.Pages, partials); err != nil {
		return fmt.Errorf("checking for template cycles: %w", err)
	}

	g := new(errgroup.Group)
	for _, rel := range pages {
		g.Go(func() error {
			return buildPage(cfg.Pages, tmpDir, rel, partials, staticManifest)
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	if err := buildSitemap(cfg.In, tmpDir, pages, cfg.BaseURL); err != nil {
		return fmt.Errorf("building sitemap: %w", err)
	}
	if err := buildRobots(cfg.In, tmpDir, cfg.BaseURL); err != nil {
		return fmt.Errorf("building robots.txt: %w", err)
	}

	oldTmp, err := os.MkdirTemp(parent, ".build-old-*")
	if err != nil {
		return fmt.Errorf("creating old-dir placeholder: %w", err)
	}
	os.Remove(oldTmp)

	if err := os.Rename(cfg.Out, oldTmp); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("moving old output directory aside: %w", err)
	}
	if err := renameFallback(tmpDir, cfg.Out); err != nil {
		_ = os.Rename(oldTmp, cfg.Out)
		return fmt.Errorf("swapping build directory into place: %w", err)
	}
	if err := os.RemoveAll(oldTmp); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to remove old build dir %s: %v\n", oldTmp, err)
	}

	success = true
	fmt.Printf("build complete: %d pages -> %s\n", len(pages), cfg.Out)
	return nil
}

func renameFallback(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		var linkErr *os.LinkError
		if !errors.As(err, &linkErr) || !errors.Is(linkErr.Err, syscall.EXDEV) {
			return err
		}
		fmt.Fprintf(os.Stderr, "warning: rename across filesystems, falling back to copy (%v)\n", err)
		if err := copyDir(src, dst); err != nil {
			return fmt.Errorf("copy fallback failed: %w", err)
		}
		return os.RemoveAll(src)
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) (retErr error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer func() {
		if err := out.Close(); err != nil && retErr == nil {
			retErr = err
		}
	}()

	_, err = io.Copy(out, in)
	return err
}

type partialSource struct {
	name, src string
}

func loadPartials(dir string) ([]partialSource, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}

	var partials []partialSource
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walking partials directory: %w", err)
		}
		if d.IsDir() || filepath.Ext(d.Name()) != ".html" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading partial %s: %w", path, err)
		}
		// Use path relative to the partials root dir as the name
		name, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("computing relative path for %s: %w", path, err)
		}
		partials = append(partials, partialSource{name: name, src: string(src)})
		fmt.Printf("partial: %s\n", name)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return partials, nil
}

func buildPage(pagesDir, outDir, relPage string, partials []partialSource, staticManifest map[string]string) error {
	htmlPath := filepath.Join(pagesDir, relPage)
	pageDir := filepath.Join(pagesDir, filepath.Dir(relPage))

	pageBytes, err := os.ReadFile(htmlPath)
	if err != nil {
		return fmt.Errorf("reading page: %w", err)
	}

	data, err := loadData(pagesDir, relPage)
	if err != nil {
		return fmt.Errorf("loading data: %w", err)
	}
	var tmplData any
	if len(data) > 0 {
		tmplData = data
	}

	var funcMap template.FuncMap
	funcMap = template.FuncMap{
		"static": func(name string) (string, error) {
			name = filepath.ToSlash(name)
			hashed, ok := staticManifest[name]
			if !ok {
				return "", fmt.Errorf("static asset %q not found in manifest", name)
			}
			return "/" + hashed, nil
		},
		"md": func(name string) (template.HTML, error) {
			mdPath, err := resolveMarkdown(pageDir, pagesDir, name)
			if err != nil {
				return "", err
			}
			return renderMarkdown(mdPath, funcMap, partials, tmplData, nil)
		},
	}

	tmpl, err := template.New(relPage).Funcs(funcMap).Parse(string(pageBytes))
	if err != nil {
		return fmt.Errorf("parsing template: %w", err)
	}
	for _, p := range partials {
		if _, err := tmpl.New(p.name).Parse(p.src); err != nil {
			return fmt.Errorf("parsing partial %s for page %s: %w", p.name, relPage, err)
		}
	}

	outPath := filepath.Join(outDir, relPage)
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	outFile, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer outFile.Close()

	if err := tmpl.ExecuteTemplate(outFile, relPage, tmplData); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}

	fmt.Printf("built: %s -> %s\n", htmlPath, outPath)
	return nil
}

func resolveMarkdown(pageDir, pagesDir, name string) (string, error) {
	var candidates []string
	if strings.HasPrefix(name, "/") {
		candidates = []string{filepath.Join(pagesDir, name[1:])}
	} else {
		candidates = []string{
			filepath.Join(pageDir, name),
			filepath.Join(pagesDir, name),
		}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("markdown file %q not found", name)
}

func renderMarkdown(path string, funcMap template.FuncMap, partials []partialSource, data any, visiting []string) (template.HTML, error) {
	if slices.Contains(visiting, path) {
		return "", fmt.Errorf("cycle detected in markdown: %s", strings.Join(append(append([]string{}, visiting...), path), " -> "))
	}
	visiting = append(append([]string{}, visiting...), path)

	src, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading markdown file %s: %w", path, err)
	}

	tmpl, err := template.New(filepath.Base(path)).Funcs(funcMap).Parse(string(src))
	if err != nil {
		return "", fmt.Errorf("parsing markdown template %s: %w", path, err)
	}
	for _, p := range partials {
		if _, err := tmpl.New(p.name).Parse(p.src); err != nil {
			return "", fmt.Errorf("parsing partial %s for markdown %s: %w", p.name, path, err)
		}
	}

	var rendered strings.Builder
	if err := tmpl.Execute(&rendered, data); err != nil {
		return "", fmt.Errorf("executing markdown template %s: %w", path, err)
	}

	var buf strings.Builder
	if err := gmRenderer.Convert([]byte(rendered.String()), &buf); err != nil {
		return "", fmt.Errorf("rendering markdown file %s: %w", path, err)
	}

	return template.HTML(buf.String()), nil
}

func loadData(pagesDir, relPage string) (map[string]any, error) {
	pageDir := filepath.Dir(relPage)
	segments := []string{"."}
	if pageDir != "." {
		parts := strings.Split(pageDir, string(filepath.Separator))
		for i := range parts {
			segments = append(segments, filepath.Join(parts[:i+1]...))
		}
	}

	result := make(map[string]any)
	for _, seg := range segments {
		result, _ = readAndMergeJSON(result, filepath.Join(pagesDir, seg, "_data.json"))
	}

	pageName := strings.TrimSuffix(filepath.Base(relPage), ".html")
	return readAndMergeJSON(result, filepath.Join(pagesDir, pageDir, pageName+".json"))
}

func readAndMergeJSON(base map[string]any, path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return base, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading data file %s: %w", path, err)
	}
	var overlay map[string]any
	if err := json.Unmarshal(b, &overlay); err != nil {
		return nil, fmt.Errorf("parsing data file %s: %w", path, err)
	}
	return mergeMaps(base, overlay), nil
}

func mergeMaps(base, overlay map[string]any) map[string]any {
	out := make(map[string]any, len(base))
	maps.Copy(out, base)
	for k, ov := range overlay {
		if bv, ok := out[k]; ok {
			if bMap, ok1 := bv.(map[string]any); ok1 {
				if oMap, ok2 := ov.(map[string]any); ok2 {
					out[k] = mergeMaps(bMap, oMap)
					continue
				}
			}
		}
		out[k] = ov
	}
	return out
}

func copyStaticAssets(staticDir, outDir string) (map[string]string, error) {
	if _, err := os.Stat(staticDir); os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("creating output dir for static assets: %w", err)
	}

	var mu sync.Mutex
	manifest := map[string]string{}
	g := new(errgroup.Group)

	err := filepath.Walk(staticDir, func(srcPath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		relPath, err := filepath.Rel(staticDir, srcPath)
		if err != nil {
			return fmt.Errorf("computing relative path for %s: %w", srcPath, err)
		}
		g.Go(func() error {
			hashedRel, err := copyWithHash(srcPath, relPath, outDir)
			if err != nil {
				return err
			}
			mu.Lock()
			manifest[filepath.ToSlash(relPath)] = hashedRel
			mu.Unlock()
			fmt.Printf("static: %s -> %s\n", relPath, hashedRel)
			return nil
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking static directory: %w", err)
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return manifest, nil
}

func copyWithHash(srcPath, relPath, outDir string) (string, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("opening static file %s: %w", srcPath, err)
	}
	defer src.Close()

	tmp, err := os.CreateTemp(outDir, ".static-tmp-*")
	if err != nil {
		return "", fmt.Errorf("creating temp file for %s: %w", srcPath, err)
	}
	tmpPath := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpPath)
	}()

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), src); err != nil {
		return "", fmt.Errorf("copying static file %s: %w", srcPath, err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("flushing static file %s: %w", srcPath, err)
	}

	ext := filepath.Ext(relPath)
	hashedRel := filepath.ToSlash(strings.TrimSuffix(relPath, ext)) + "." + hex.EncodeToString(h.Sum(nil))[:12] + ext
	dstPath := filepath.Join(outDir, hashedRel)

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return "", fmt.Errorf("creating output dir for %s: %w", hashedRel, err)
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return "", fmt.Errorf("renaming temp file to %s: %w", dstPath, err)
	}
	return hashedRel, nil
}

func extractCalls(name, src string) (templateCalls, mdCalls []string, _ error) {
	tmpl, err := template.New(name).Funcs(template.FuncMap{
		"static": func(string) (string, error)        { return "", nil },
		"md":     func(string) (template.HTML, error) { return "", nil },
	}).Parse(src)
	if err != nil {
		return nil, nil, err
	}
	t := tmpl.Lookup(name)
	if t == nil || t.Tree == nil || t.Tree.Root == nil {
		return nil, nil, nil
	}

	var walk func(ttparse.Node)
	walk = func(n ttparse.Node) {
		switch n := n.(type) {
		case *ttparse.TemplateNode:
			templateCalls = append(templateCalls, n.Name)
		case *ttparse.ActionNode:
			if n.Pipe != nil {
				for _, cmd := range n.Pipe.Cmds {
					if len(cmd.Args) >= 2 {
						if id, ok := cmd.Args[0].(*ttparse.IdentifierNode); ok && id.Ident == "md" {
							if s, ok := cmd.Args[1].(*ttparse.StringNode); ok {
								mdCalls = append(mdCalls, s.Text)
							}
						}
					}
				}
			}
		case *ttparse.ListNode:
			for _, child := range n.Nodes {
				walk(child)
			}
		case *ttparse.IfNode:
			walk(n.List)
			if n.ElseList != nil {
				walk(n.ElseList)
			}
		case *ttparse.RangeNode:
			walk(n.List)
			if n.ElseList != nil {
				walk(n.ElseList)
			}
		case *ttparse.WithNode:
			walk(n.List)
			if n.ElseList != nil {
				walk(n.ElseList)
			}
		}
	}
	walk(t.Tree.Root)
	return templateCalls, mdCalls, nil
}

func checkCycles(pages []string, pagesDir string, partials []partialSource) error {
	known := make(map[string]struct{}, len(pages)+len(partials))
	for _, rel := range pages {
		known[rel] = struct{}{}
	}
	for _, p := range partials {
		known[p.name] = struct{}{}
	}

	graph := make(map[string][]string)
	mdScanned := make(map[string]struct{})
	var mdQueue []string

	addEdges := func(nodeID, src, pageDir string) error {
		tmplCalls, mdCallNames, err := extractCalls(nodeID, src)
		if err != nil {
			return err
		}
		for _, callee := range tmplCalls {
			if _, ok := known[callee]; ok {
				graph[nodeID] = append(graph[nodeID], callee)
			}
		}
		for _, rawName := range mdCallNames {
			absPath, err := resolveMarkdown(pageDir, pagesDir, rawName)
			if err != nil {
				continue
			}
			known[absPath] = struct{}{}
			graph[nodeID] = append(graph[nodeID], absPath)
			if _, seen := mdScanned[absPath]; !seen {
				mdQueue = append(mdQueue, absPath)
			}
		}
		return nil
	}

	for _, rel := range pages {
		src, err := os.ReadFile(filepath.Join(pagesDir, rel))
		if err != nil {
			return fmt.Errorf("reading page %s for cycle detection: %w", rel, err)
		}
		if err := addEdges(rel, string(src), filepath.Join(pagesDir, filepath.Dir(rel))); err != nil {
			return fmt.Errorf("parsing page %s for cycle detection: %w", rel, err)
		}
	}
	for _, p := range partials {
		if err := addEdges(p.name, p.src, pagesDir); err != nil {
			return fmt.Errorf("parsing partial %s for cycle detection: %w", p.name, err)
		}
	}
	for len(mdQueue) > 0 {
		absPath := mdQueue[0]
		mdQueue = mdQueue[1:]
		if _, seen := mdScanned[absPath]; seen {
			continue
		}
		mdScanned[absPath] = struct{}{}
		src, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		if err := addEdges(absPath, string(src), filepath.Dir(absPath)); err != nil {
			return fmt.Errorf("parsing markdown %s for cycle detection: %w", absPath, err)
		}
	}

	type colour byte
	const (
		grey  colour = 1
		black colour = 2
	)
	colours := make(map[string]colour, len(graph))
	path := make([]string, 0, len(graph))

	var dfs func(string) error
	dfs = func(node string) error {
		colours[node] = grey
		path = append(path, node)
		for _, neighbour := range graph[node] {
			switch colours[neighbour] {
			case black:
				continue
			case grey:
				start := slices.Index(path, neighbour)
				return fmt.Errorf("cycle detected in templates: %s", strings.Join(append(path[start:], neighbour), " -> "))
			default:
				if err := dfs(neighbour); err != nil {
					return err
				}
			}
		}
		path = path[:len(path)-1]
		colours[node] = black
		return nil
	}

	for node := range graph {
		if colours[node] != black {
			if err := dfs(node); err != nil {
				return err
			}
		}
	}
	return nil
}

func buildSitemap(inDir, outDir string, pages []string, baseURL string) error {
	if src := filepath.Join(inDir, "sitemap.xml"); fileExists(src) {
		fmt.Println("sitemap: using project-root sitemap.xml")
		return copyFile(src, filepath.Join(outDir, "sitemap.xml"))
	}

	baseURL = strings.TrimRight(baseURL, "/")
	now := time.Now().UTC().Format("2006-01-02")

	type urlEntry struct{ Loc, LastMod string }
	entries := make([]urlEntry, 0, len(pages))
	for _, rel := range pages {
		loc := filepath.ToSlash(rel)
		if filepath.Base(rel) == "index.html" {
			loc = filepath.ToSlash(filepath.Dir(rel)) + "/"
			if loc == "./" {
				loc = "/"
			}
		}
		entries = append(entries, urlEntry{baseURL + "/" + strings.TrimPrefix(loc, "/"), now})
	}

	t, err := texttmpl.New("sitemap").Parse(embedded.SitemapTempl)
	if err != nil {
		return fmt.Errorf("parsing sitemap template: %w", err)
	}
	f, err := os.Create(filepath.Join(outDir, "sitemap.xml"))
	if err != nil {
		return fmt.Errorf("creating sitemap.xml: %w", err)
	}
	defer f.Close()
	if err := t.Execute(f, entries); err != nil {
		return fmt.Errorf("executing sitemap template: %w", err)
	}

	fmt.Printf("sitemap: generated %d URLs -> sitemap.xml\n", len(entries))
	return nil
}

func buildRobots(inDir, outDir, baseURL string) error {
	if src := filepath.Join(inDir, "robots.txt"); fileExists(src) {
		fmt.Println("robots: using project-root robots.txt")
		return copyFile(src, filepath.Join(outDir, "robots.txt"))
	}

	content := "User-agent: *\nAllow: /\n"
	if baseURL != "" {
		content += fmt.Sprintf("Sitemap: %s/sitemap.xml\n", strings.TrimRight(baseURL, "/"))
	}
	if err := os.WriteFile(filepath.Join(outDir, "robots.txt"), []byte(content), 0644); err != nil {
		return fmt.Errorf("writing robots.txt: %w", err)
	}

	fmt.Println("robots: generated robots.txt")
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
