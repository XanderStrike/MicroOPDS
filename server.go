package main

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func serve(addr, baseDir string, catalog *SafeCatalog, user, pass string) error {
	mux := http.NewServeMux()

	// OPDS catalog endpoint
	mux.HandleFunc("/catalog", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml;profile=opds-catalog;kind=acquisition; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")

		cat := catalog.Get()
		baseURL := getBaseURL(r)
		feed := generateFeed(baseURL, "MicroOPDS Catalog", cat.Books, time.Now())

		data, err := feed.XML()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Write(data)
	})

	// OpenSearch description document
	mux.HandleFunc("/search.xml", func(w http.ResponseWriter, r *http.Request) {
		baseURL := getBaseURL(r)
		w.Header().Set("Content-Type", "application/opensearchdescription+xml; charset=utf-8")
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/">
  <ShortName>MicroOPDS</ShortName>
  <Description>Search MicroOPDS catalog</Description>
  <InputEncoding>UTF-8</InputEncoding>
  <OutputEncoding>UTF-8</OutputEncoding>
  <Url type="application/atom+xml;profile=opds-catalog;kind=acquisition" template="%s/search?q={searchTerms}"/>
</OpenSearchDescription>`, baseURL)
	})

	// Search endpoint
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			http.Error(w, "missing query parameter 'q'", http.StatusBadRequest)
			return
		}

		cat := catalog.Get()
		// Simple case-insensitive string matching on title, author, and description
		query = strings.ToLower(query)
		var results []Book
		for _, book := range cat.Books {
			matched := false
			if strings.Contains(strings.ToLower(book.Title), query) {
				matched = true
			} else {
				for _, author := range book.Authors {
					if strings.Contains(strings.ToLower(author), query) {
						matched = true
						break
					}
				}
			}
			if !matched && strings.Contains(strings.ToLower(book.Description), query) {
				matched = true
			}
			if matched {
				results = append(results, book)
			}
		}

		w.Header().Set("Content-Type", "application/atom+xml;profile=opds-catalog;kind=acquisition; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")

		baseURL := getBaseURL(r)
		feed := generateFeed(baseURL, "Search: "+r.URL.Query().Get("q"), results, time.Now())

		data, err := feed.XML()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Write(data)
	})

	// Root shows info page
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			cat := catalog.Get()
			// count unique authors
			authors := make(map[string]bool)
			for _, b := range cat.Books {
				for _, a := range b.Authors {
					authors[a] = true
				}
			}

			baseURL := getBaseURL(r)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>MicroOPDS</title></head>
<body>
<h1>MicroOPDS</h1>
<p>%d authors, %d books in the catalog</p>
<p>add this to your OPDS client: <a href="%s/catalog">%s/catalog</a></p>
</body>
</html>`, len(authors), len(cat.Books), baseURL, baseURL)
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
		w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(bookPath)+"\"")

		http.ServeContent(w, r, filepath.Base(bookPath), stat.ModTime(), f)
	})

	// Cover image endpoint
	mux.HandleFunc("/covers/", func(w http.ResponseWriter, r *http.Request) {
		// Get epub path and cover path from URL
		relPath := strings.TrimPrefix(r.URL.Path, "/covers/")
		coverPath := r.URL.Query().Get("path")
		if coverPath == "" {
			http.Error(w, "missing cover path", http.StatusBadRequest)
			return
		}

		bookPath := filepath.Join(baseDir, relPath)

		// Security: ensure the epub path is within baseDir
		absPath, err := filepath.Abs(bookPath)
		if err != nil || !strings.HasPrefix(absPath, baseDir) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		// Open the epub as a zip
		zr, err := zip.OpenReader(bookPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer zr.Close()

		// Find and serve the cover image
		for _, f := range zr.File {
			fp := f.Name
			if fp == coverPath || strings.HasSuffix(fp, coverPath) {
				rc, err := f.Open()
				if err != nil {
					http.Error(w, "failed to open cover", 500)
					return
				}
				defer rc.Close()

				// Detect content type from file extension
				ct := mime.TypeByExtension(filepath.Ext(fp))
				if ct == "" {
					ct = "image/jpeg" // default
				}
				w.Header().Set("Content-Type", ct)
				w.Header().Set("Cache-Control", "public, max-age=86400")
				io.Copy(w, rc)
				return
			}
		}

		http.NotFound(w, r)
	})

	handler := http.Handler(mux)
	if user != "" && pass != "" {
		handler = basicAuth(mux, user, pass)
	}
	handler = loggingMiddleware(handler)

	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return srv.ListenAndServe()
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &loggingWriter{w: w}
		next.ServeHTTP(lw, r)
		if !strings.HasPrefix(r.URL.Path, "/covers") {
			uri := r.URL.Path
			if r.URL.RawQuery != "" {
				uri += "?" + r.URL.RawQuery
			}
			log.Printf("%s %s %d %v", r.Method, uri, lw.status, time.Since(start))
		}
	})
}

type loggingWriter struct {
	w      http.ResponseWriter
	status int
}

func (lw *loggingWriter) Header() http.Header         { return lw.w.Header() }
func (lw *loggingWriter) Write(b []byte) (int, error) {
	if lw.status == 0 {
		lw.status = 200
	}
	return lw.w.Write(b)
}
func (lw *loggingWriter) WriteHeader(status int) {
	lw.status = status
	lw.w.WriteHeader(status)
}

func basicAuth(next http.Handler, user, pass string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != user || p != pass {
			w.Header().Set("WWW-Authenticate", `Basic realm="MicroOPDS"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func getBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}


