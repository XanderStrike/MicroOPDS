package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
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
}

type Catalog struct {
	Books []Book
	mu    sync.RWMutex
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

			metadata, err := epub.GetMetadataFromFile(path)
			if err != nil {
				log.Printf("error reading %s: %v", path, err)
				return
			}

			book := Book{
				AbsPath: path,
				RelPath: relPath,
				ModTime: info.ModTime(),
				Size:    info.Size(),
			}

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
	if err := serve(addr, absDir, catalog); err != nil {
		log.Fatal(err)
	}
}
