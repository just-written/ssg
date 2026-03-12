// entry point and some CLI logic
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
)

// refactor this if it gets annoying i guess
//go:embed embedded/help/help.txt
var helpText string
//go:embed embedded/help/init.txt
var helpInitText string
//go:embed embedded/help/build.txt
var helpBuildText string
//go:embed embedded/help/dev.txt
var helpDevText string
//go:embed embedded/help/list.txt
var helpListText string

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Invalid command: ")
		fmt.Println("Run 'ssg help' for help.")
		return
	}

	switch os.Args[1] {
	case "help":
		fmt.Print(helpText)
	case "init":
		cmd := flag.NewFlagSet("init", flag.ExitOnError)
		outDir := cmd.String("dir", "ssg-project", "New projects directory. (def. ssg-project)")
		templ := cmd.String("templ", "default", "Template to use. (def. bare)")
		help := cmd.Bool("help", false, "Show build help")
		cmd.Parse(os.Args[2:])

		if *help {
			fmt.Print(helpInitText)
			return
		}

		if err := ssgInit(*outDir, *templ); err != nil {
			fmt.Fprintf(os.Stderr, "command 'init' error: %v\n", err)
		}
	case "build":
		// This really sucks. i'm not sure how other cli tools manage this but it's kinda ugly here
		cmd := flag.NewFlagSet("build", flag.ExitOnError)
		out := cmd.String("out", "dist", "Build directory (def. dist/)")
		in := cmd.String("in", ".", "Source directory (def. pages/)")
		baseURL := cmd.String("base-url", "", "Base URL for sitemap.")
		validateAssets := cmd.Bool("validate-assets", false, "Validate assets exist.")
		checkLinks := cmd.Bool("check-links", false, "Check if internal links are valid.")
		verbose := cmd.Bool("verbose", false, "Print data keys available to each page.")
		quiet := cmd.Bool("quiet", false, "Suppress build messages.")
		watch := cmd.Bool("watch", false, "Watch for changes and rebuild.")
		force := cmd.Bool("force", false, "Force complete rebuild, ignores dependency graph.")
		help := cmd.Bool("help", false, "Show build help")
		cmd.Parse(os.Args[2:])

		flags := BuildFlags{
			BuildDir:       *out,
			SrcDir:         *in,
			BaseURL:        *baseURL,
			ValidateAssets: *validateAssets,
			CheckLinks:     *checkLinks,
			Verbose:        *verbose,
			Quiet:          *quiet,
			Force: 			*force,
		}

		if *help {
			fmt.Print(helpBuildText)
			return
		}

		if *watch {
			if err := ssgWatch(flags, nil); err != nil {
				fmt.Fprintf(os.Stderr, "command 'build' error: %v\n", err)
			}
		} else {
			if err := ssgBuild(flags); err != nil {
				fmt.Fprintf(os.Stderr, "command 'build' error: %v\n", err)
			}
		}
	case "dev":
		cmd := flag.NewFlagSet("dev", flag.ExitOnError)
		out := cmd.String("out", "dist", "Build directory (def. dist/)")
		in := cmd.String("in", ".", "Source directory (def. .)")
		baseURL := cmd.String("base-url", "", "Base URL for sitemap.")
		verbose := cmd.Bool("verbose", false, "Print data keys available to each page.")
		port := cmd.Int("port", 8788, "Wrangler dev server port.")
		wranglerBin := cmd.String("wrangler", "wrangler", "Path to the wrangler binary.")
		help := cmd.Bool("help", false, "Show build help")
		cmd.Parse(os.Args[2:])

		if *help {
			fmt.Print(helpDevText)
			return
		}

		flags := DevFlags{
			BuildFlags: BuildFlags{
				BuildDir: *out,
				SrcDir:   *in,
				BaseURL:  *baseURL,
				Verbose:  *verbose,
				Quiet:    false,
			},

			WranglerBin:  *wranglerBin,
			WranglerPort: *port,
			WranglerArgs: cmd.Args(),
		}

		if err := ssgDev(flags); err != nil {
			fmt.Fprintf(os.Stderr, "command 'dev' error: %v\n", err)
		}
	case "list":
		cmd := flag.NewFlagSet("list", flag.ExitOnError)
		in := cmd.String("dir", ".", "Source directory.")
		help := cmd.Bool("help", false, "Show list help")
		cmd.Parse(os.Args[2:])

		if *help {
			fmt.Print(helpListText)
			return
		}

		if err := ssgList(*in); err != nil {
			fmt.Fprintf(os.Stderr, "command 'list' error: %v\n", err)
		}
	default:
		fmt.Printf("Invalid command: %s\n", os.Args[1])
		fmt.Println("Run 'ssg help' for help.")
	}
}
