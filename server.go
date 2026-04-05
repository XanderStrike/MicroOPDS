package main

import (
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func serve(addr, baseDir string, catalog *Catalog) error {
	mux := http.NewServeMux()

	// OPDS catalog endpoint
	mux.HandleFunc("/catalog", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml;profile=opds-catalog;kind=acquisition; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")

		baseURL := getBaseURL(r)
		feed := generateFeed(baseURL, "MiniOPDS Catalog", catalog.Books, time.Now())
		
		data, err := feed.XML()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Write(data)
	})

	// Root redirects to catalog
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/catalog", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	// Book download endpoint
	mux.HandleFunc("/books/", func(w http.ResponseWriter, r *http.Request) {
		// Decode the URL-encoded relative path
		relPath := strings.TrimPrefix(r.URL.Path, "/books/")
		bookPath := filepath.Join(baseDir, relPath)

		// Security: ensure the path is within baseDir
		absPath, err := filepath.Abs(bookPath)
		if err != nil || !strings.HasPrefix(absPath, baseDir) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		// Open and serve the file
		f, err := os.Open(bookPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()

		stat, err := f.Stat()
		if err != nil {
			http.Error(w, "file error", 500)
			return
		}

		// Set proper content type
		mime.AddExtensionType(".epub", "application/epub+zip")
		w.Header().Set("Content-Type", "application/epub+zip")
		w.Header().Set("Content-Length", stringOrInt(stat.Size()))
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(bookPath)+"\"")

		http.ServeContent(w, r, filepath.Base(bookPath), stat.ModTime(), f)
	})

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return srv.ListenAndServe()
}

func getBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func stringOrInt(i int64) string {
	var buf [20]byte
	n := len(buf)
	for i > 0 {
		n--
		buf[n] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[n:])
}

// io.ReadSeeker adapter for http.ServeContent
type readSeeker struct {
	io.Reader
	io.Seeker
}
