# Static Site Generator
Made to be really simple and work off of my own existing knowledge of golang html templates instead of making me 
learn a whole new system. The only difference between normal golang html templates is the addition of the `md` and `static`
functions (explained below).

### Quickstart
I use it as a nix flake in nix shells for company projects. If you want you can build it with go if you don't use nix.

For CLI usage run `ssg --help` or `ssg [command] --help`

## How it works
An SSG project is only required to have a single pages directory to be built, but most of the time you want to have
a partials and a static directory as well. The ssg.toml config file is not required either, its just for storing flags if you dont
want to type them in every time.

### Pages
The only required directory. It contains the HTML, JSON, and Markdown files required to build a page. There is no required layout on how
to organize your pages, just be aware that the pages are built with the same directory layout and name as the HTML file
(pages/subdir/index.html + pages/subdir/index.json -> dist/subdir/index.html).

An HTML file is just a golang template, you can use all the same golang templating stuff you would expect. For more info on 
golang html/template stuff, click [here](https://pkg.go.dev/html/template). 
No page can reference another page or its template definitions. Globally accessable templates are stored in the partials directory.

JSON files provide data for the golang template (the HTML files). A JSON file with the same name as an HTML file is page specific 
and only used for that page. JSON files with the name \_data.json are global to that directory and all subdirectories, merged with the 
closest files replacing any duplicate keys (eg. \_data.json + subdir/\_data.json + index.json + index.html -> dist/index.html). No JSON 
data is required to build a page, and missing data in templates is ignored.

Markdown files are good for writing content for blogs, docs, or any other form of rambling. Golang template functions also work from 
within Markdown files. Markdown recursion is also detected at build time. To render a Markdown file see the md function below.

### Partials
The partials directory contains only HTML files. Each file is register as a template and can be accessed by any other html file,
both pages and partials. Template recursion is detected at build time and not allowed to prevent infinite loops.

### Static
Files in this directory are copied 1 to 1 to the build directory, with the only change being a hash added to the file name for cache busting.

Mainly used for images, js, etc.

### ssg.toml
The ssg.toml file sets flags for the commands in the cli using the TOML sections. CLI flags override the values in the config.

## Template Functions
The only change from the default golang html/template is the additional of the md and static functions.

### {{md}}
The md function renders markdown in place, searching the directory of the file it was called from first and then the pages root.

`{{md "markdown.md"}}` -> If called in subdir/index.html it searches for subdir/mardown.md first and if it doesn't exist it
searches for /markdown.md.

`{{md "/markdown.md"}}` -> Skips the relative search and goes right to the path from the pages root.

### {{static}}
The static function resolves the hashed path generated during build.

`{{static "asset.js"}}` -> resolves to hashed path (eg. assets.e3b0c44298fc.js)`
