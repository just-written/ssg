package embedded

import "embed"

//go:embed files/sitemap-templ.xml
var SitemapTempl string

//go:embed all:files/project-templates/*
var TemplateFS embed.FS
