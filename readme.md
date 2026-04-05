# miniopds

a minimal opds 1.2 catalog server for epub files.

## usage

```
miniopds -dir /path/to/epubs -port 8080
```

opens a catalog at `http://localhost:8080/catalog`

## opds 1.2 compliance

- serves a single acquisition feed as the catalog root
- includes atom + dublin core metadata from epub files
- provides download links with proper content types
- supports parallel scanning for fast startup
