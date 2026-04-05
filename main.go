package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pirmd/epub"
)

type Book struct {
	RelPath     string // relative to base dir
	AbsPath     string // absolute path for file access
	Title       string
	Authors     []string
	Identifier  string
	Publisher   string
	Language    string
	Description string
	Subjects    []string
	Rights      string
	Date        string
	ModTime     time.Time
	Size        int64
	CoverPath   string // path to cover image within epub
	CoverType   string // mime type of cover image
}

type Catalog struct {
	Books []Book
	mu    sync.RWMutex
}

// findCover locates the cover image in an EPUB package
func findCover(opf *epub.PackageDocument) (path string, mediaType string) {
	if opf.Manifest == nil {
		return
	}

	// EPUB 3: look for item with properties="cover-image"
	for _, item := range opf.Manifest.Items {
		if strings.Contains(item.Properties, "cover-image") {
			return item.Href, item.MediaType
		}
	}

	// EPUB 2: look for <meta name="cover" content="item-id"/>
	var coverID string
	for _, meta := range opf.Metadata.Meta {
		if meta.Name == "cover" {
			coverID = meta.Content
			break
		}
	}

	if coverID != "" {
		// Try exact match first
		for _, item := range opf.Manifest.Items {
			if item.ID == coverID {
				return item.Href, item.MediaType
			}
		}
		// Fallback: some EPUBs have malformed metadata where coverID="cover" but id="cover.jpg"
		for _, item := range opf.Manifest.Items {
			if strings.HasPrefix(item.ID, coverID) || strings.HasSuffix(item.ID, coverID) {
				return item.Href, item.MediaType
			}
		}
	}

	// Last resort: look for common cover image names in manifest
	for _, item := range opf.Manifest.Items {
		lowerHref := strings.ToLower(item.Href)
		if strings.Contains(lowerHref, "cover") && strings.HasPrefix(item.MediaType, "image/") {
			return item.Href, item.MediaType
		}
	}

	return
}

func scan(dir string) (*Catalog, error) {
	var books []Book
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8) // limit concurrent file opens

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".epub" {
			return nil
		}

		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		wg.Add(1)
		go func() {
			sem <- struct{}{}
			defer func() { <-sem; wg.Done() }()

			// Open epub to get both metadata and package for cover
			e, err := epub.Open(path)
			if err != nil {
				log.Printf("error reading %s: %v", path, err)
				return
			}
			defer e.Close()

			metadata, err := e.Information()
			if err != nil {
				log.Printf("error reading metadata %s: %v", path, err)
				return
			}

			opf, err := e.Package()
			if err != nil {
				log.Printf("error reading package %s: %v", path, err)
				return
			}

			book := Book{
				AbsPath: path,
				RelPath: relPath,
				ModTime: info.ModTime(),
				Size:    info.Size(),
			}

			// Extract cover info
			book.CoverPath, book.CoverType = findCover(opf)

			if len(metadata.Title) > 0 {
				book.Title = metadata.Title[0]
			} else {
				book.Title = filepath.Base(path)
			}

			for _, a := range metadata.Creator {
				book.Authors = append(book.Authors, a.FullName)
			}

			if len(metadata.Identifier) > 0 {
				book.Identifier = metadata.Identifier[0].Value
			}

			if len(metadata.Publisher) > 0 {
				book.Publisher = metadata.Publisher[0]
			}
			if len(metadata.Language) > 0 {
				book.Language = metadata.Language[0]
			}
			if len(metadata.Description) > 0 {
				book.Description = metadata.Description[0]
			}
			book.Subjects = metadata.Subject
			if len(metadata.Rights) > 0 {
				book.Rights = metadata.Rights[0]
			}
			if len(metadata.Date) > 0 {
				book.Date = metadata.Date[0].Stamp
			}

			mu.Lock()
			books = append(books, book)
			mu.Unlock()
		}()

		return nil
	})

	wg.Wait()
	return &Catalog{Books: books}, err
}

func main() {
	dir := flag.String("dir", ".", "directory containing epub files")
	port := flag.Int("port", 8080, "port to listen on")
	flag.Parse()

	user := os.Getenv("USER")
	pass := os.Getenv("PASS")

	absDir, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatalf("invalid directory: %v", err)
	}

	log.Printf("scanning %s...", absDir)
	start := time.Now()
	catalog, err := scan(absDir)
	if err != nil {
		log.Fatalf("scan failed: %v", err)
	}
	log.Printf("found %d books in %v", len(catalog.Books), time.Since(start))

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("serving OPDS catalog at http://localhost%s/", addr)
	if err := serve(addr, absDir, catalog, user, pass); err != nil {
		log.Fatal(err)
	}
}
