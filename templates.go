// Copyright 2026 Golinky
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"html/template"
)

var (
	homeTmpl    *template.Template
	detailTmpl  *template.Template
	successTmpl *template.Template
	deleteTmpl  *template.Template
	searchTmpl  *template.Template
	helpTmpl    *template.Template
)

type visitData struct {
	Short     string
	NumClicks int
}

type homeData struct {
	Short  string
	Long   string
	Clicks []visitData
}

type deleteData struct {
	Short string
	Long  string
}

type detailData struct {
	Link *Link
}

func init() {
	homeTmpl = newTemplate("base.html", "home.html")
	detailTmpl = newTemplate("base.html", "detail.html")
	successTmpl = newTemplate("base.html", "success.html")
	deleteTmpl = newTemplate("base.html", "delete.html")
	searchTmpl = newTemplate("base.html", "search.html")
	helpTmpl = newTemplate("base.html", "help.html")
}

var tmplFuncs = template.FuncMap{
	"go": func() string {
		return defaultHostname
	},
}

func newTemplate(files ...string) *template.Template {
	if len(files) == 0 {
		return nil
	}
	tf := make([]string, 0, len(files))
	for _, f := range files {
		tf = append(tf, "tmpl/"+f)
	}
	t := template.New(files[0]).Funcs(tmplFuncs)
	return template.Must(t.ParseFS(embeddedFS, tf...))
}
