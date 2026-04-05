# microopds

a minimal opds 1.2 catalog server for epub files.

for when [tinyopds](https://github.com/sensboston/tinyopds) is just too much

## features

- standards compliant OPDS feed of epubs in a folder
- substring search
- cover extraction
- basic auth
- all in a lightweight docker image

## unfeatures

MicroOPDS does not and never will implement

- a web interface
- a reading interface
- external metadata
- formats other than epub
- upload
- integration with calibre or anything else
- any other features you can think of

## usage

```
docker compose up -d
```

mounts `./books` and serves at `http://0.0.0.0:8080/catalog`

### basic auth

uncomment `USER` and `PASS` in docker-compose.yml to enable basic auth.
