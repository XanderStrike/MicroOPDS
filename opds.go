package main

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"time"
)

const (
	nsAtom  = "http://www.w3.org/2005/Atom"
	nsDC    = "http://purl.org/dc/terms/"
	nsOPDS  = "http://opds-spec.org/2010/catalog"
	nsThr   = "http://purl.org/syndication/thread/1.0"
)

type Feed struct {
	XMLName xml.Name `xml:"http://www.w3.org/2005/Atom feed"`
	XMLNS   string   `xml:"xmlns:dc,attr"`
	ID      string   `xml:"id"`
	Title   string   `xml:"title"`
	Updated string   `xml:"updated"`
	Author  *Author  `xml:"author,omitempty"`
	Links   []Link   `xml:"link"`
	Entries []Entry  `xml:"entry"`
}

type Entry struct {
	XMLName     xml.Name `xml:"entry"`
	ID          string   `xml:"id"`
	Title       string   `xml:"title"`
	Updated     string   `xml:"updated"`
	Authors     []Author `xml:"author,omitempty"`
	Published   string   `xml:"published,omitempty"`
	Rights      string   `xml:"rights,omitempty"`
	Summary     string   `xml:"summary,omitempty"`
	Content     string   `xml:"content,omitempty"`
	Links       []Link   `xml:"link"`
	Categories  []Category `xml:"category,omitempty"`
	Identifier  string   `xml:"dc:identifier,omitempty"`
	Publisher   string   `xml:"dc:publisher,omitempty"`
	Language    string   `xml:"dc:language,omitempty"`
	Issued      string   `xml:"dc:issued,omitempty"`
}

type Author struct {
	Name string `xml:"name"`
	URI  string `xml:"uri,omitempty"`
}

type Link struct {
	Rel      string `xml:"rel,attr"`
	Href     string `xml:"href,attr"`
	Type     string `xml:"type,attr,omitempty"`
	Title    string `xml:"title,attr,omitempty"`
	Length   int64  `xml:"length,attr,omitempty"`
}

type Category struct {
	Scheme string `xml:"scheme,attr,omitempty"`
	Term   string `xml:"term,attr"`
	Label  string `xml:"label,attr,omitempty"`
}

func generateFeed(baseURL, title string, books []Book, updated time.Time) *Feed {
	feed := &Feed{
		XMLNS:   "http://purl.org/dc/terms/",
		ID:      baseURL + "/",
		Title:   title,
		Updated: updated.UTC().Format(time.RFC3339),
		Author: &Author{
			Name: "MiniOPDS",
			URI:  baseURL,
		},
		Links: []Link{
			{Rel: "self", Href: baseURL + "/catalog", Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
			{Rel: "start", Href: baseURL + "/catalog", Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
		},
	}

	for _, book := range books {
		entry := generateEntry(baseURL, book)
		feed.Entries = append(feed.Entries, entry)
	}

	return feed
}

func generateEntry(baseURL string, book Book) Entry {
	// Create a stable ID from file path or identifier
	id := book.Identifier
	if id == "" {
		id = "urn:sha1:" + hashPath(book.RelPath)
	}

	entry := Entry{
		ID:        id,
		Title:     book.Title,
		Updated:   book.ModTime.UTC().Format(time.RFC3339),
		Identifier: book.Identifier,
		Publisher:  book.Publisher,
		Language:   book.Language,
		Issued:     book.Date,
		Rights:     book.Rights,
		Summary:    book.Description,
	}

	// Add authors
	for _, name := range book.Authors {
		entry.Authors = append(entry.Authors, Author{Name: name})
	}

	// Add subjects as categories
	for _, subject := range book.Subjects {
		entry.Categories = append(entry.Categories, Category{
			Term: subject,
		})
	}

	// URL-encode the file path for the href
	encodedPath := url.PathEscape(book.RelPath)

	// Add acquisition link
	entry.Links = append(entry.Links, Link{
		Rel:    "http://opds-spec.org/acquisition",
		Href:   baseURL + "/books/" + encodedPath,
		Type:   "application/epub+zip",
		Length: book.Size,
	})

	// Add cover image links if available
	if book.CoverPath != "" {
		coverURL := baseURL + "/covers/" + encodedPath + "?path=" + url.QueryEscape(book.CoverPath)
		entry.Links = append(entry.Links,
			Link{
				Rel:  "http://opds-spec.org/image",
				Href: coverURL,
				Type: book.CoverType,
			},
			Link{
				Rel:  "http://opds-spec.org/image/thumbnail",
				Href: coverURL,
				Type: book.CoverType,
			},
		)
	}

	return entry
}

func (f *Feed) XML() ([]byte, error) {
	output, err := xml.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, err
	}
	return []byte(xml.Header + string(output)), nil
}

func hashPath(path string) string {
	// Simple hash for generating stable IDs
	h := uint32(0)
	for _, c := range path {
		h = h*31 + uint32(c)
	}
	return fmt.Sprintf("%x", h)
}
