FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o microopds .

FROM gcr.io/distroless/static-debian12
COPY --from=builder /app/microopds /usr/local/bin/microopds
EXPOSE 8080
CMD ["microopds", "-dir", "/books", "-port", "8080"]
