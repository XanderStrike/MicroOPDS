package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/pirmd/epub"
)

type Book struct {
	RelPath     string // relative to base dir
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
}

type SafeCatalog struct {
	mu   sync.RWMutex
	cat  *Catalog
	dir  string
}

func newSafeCatalog(dir string) *SafeCatalog {
	return &SafeCatalog{cat: &Catalog{}, dir: dir}
}

func (sc *SafeCatalog) Get() *Catalog {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.cat
}

func (sc *SafeCatalog) Rescan() error {
	cat, err := scan(sc.dir)
	if err != nil {
		return err
	}
	sc.mu.Lock()
	sc.cat = cat
	sc.mu.Unlock()
	return nil
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

	// sort by author (first author, or title if no author)
	sort.Slice(books, func(i, j int) bool {
		ai := ""
		if len(books[i].Authors) > 0 {
			ai = books[i].Authors[0]
		}
		aj := ""
		if len(books[j].Authors) > 0 {
			aj = books[j].Authors[0]
		}
		if ai == aj {
			return books[i].Title < books[j].Title
		}
		return ai < aj
	})

	return &Catalog{Books: books}, err
}

func watchAndRescan(catalog *SafeCatalog, dir string) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("failed to create watcher: %v", err)
		return
	}
	defer w.Close()

	// add directory and all subdirectories for recursive watching
	addDir := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return w.Add(path)
		}
		return nil
	}
	if err := filepath.Walk(dir, addDir); err != nil {
		log.Printf("failed to watch directory: %v", err)
		return
	}

	log.Printf("watching %s for changes", dir)

	// debounce rescans to avoid thrashing on multiple events
	var timer *time.Timer
	debounce := 2 * time.Second

	for {
		select {
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			// only care about epub files
			if filepath.Ext(event.Name) != ".epub" {
				// but also watch new directories
				if event.Has(fsnotify.Create) {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						w.Add(event.Name)
					}
				}
				continue
			}
			// create, write, remove, rename all trigger rescan
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounce, func() {
				log.Printf("rescanning due to %s", event.Op.String())
				start := time.Now()
				if err := catalog.Rescan(); err != nil {
					log.Printf("rescan failed: %v", err)
				} else {
					log.Printf("rescan complete: %d books in %v", len(catalog.Get().Books), time.Since(start))
				}
			})
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			log.Printf("watcher error: %v", err)
		}
	}
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

	catalog := newSafeCatalog(absDir)

	log.Printf("scanning %s...", absDir)
	start := time.Now()
	if err := catalog.Rescan(); err != nil {
		log.Fatalf("scan failed: %v", err)
	}
	log.Printf("found %d books in %v", len(catalog.Get().Books), time.Since(start))

	// start filesystem watcher
	go watchAndRescan(catalog, absDir)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("serving OPDS catalog at http://localhost%s/", addr)
	if err := serve(addr, absDir, catalog, user, pass); err != nil {
		log.Fatal(err)
	}
}
