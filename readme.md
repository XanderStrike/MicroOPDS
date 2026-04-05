# miniopds

a minimal opds 1.2 catalog server for epub files.

## usage

```
docker compose up -d
```

mounts `./books` and serves at `http://localhost:8080/catalog`

### basic auth

uncomment `USER` and `PASS` in docker-compose.yml to enable basic auth.
