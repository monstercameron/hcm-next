package controller

import (
	"net/http"
	"path/filepath"
)

// HomeController handles requests for the home page
type HomeController struct {
	staticDir string
}

// NewHomeController creates a new instance of HomeController
func NewHomeController(staticDir string) *HomeController {
	return &HomeController{
		staticDir: staticDir,
	}
}

// ServeHome serves the static index.html file
func (hc *HomeController) ServeHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	indexPath := filepath.Join(hc.staticDir, "index.html")
	http.ServeFile(w, r, indexPath)
}