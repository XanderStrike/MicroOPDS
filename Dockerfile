FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o miniopds .

FROM alpine:3.19
COPY --from=builder /app/miniopds /usr/local/bin/miniopds
EXPOSE 8080
CMD ["miniopds", "-dir", "/books", "-port", "8080"]
