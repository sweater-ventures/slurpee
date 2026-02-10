FROM golang:1.25-alpine AS build

RUN apk add --no-cache make gzip zstd

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go tool templ generate && make
RUN go install github.com/rubenv/sql-migrate/sql-migrate@v1.8.1

FROM alpine:3.21

WORKDIR /app
COPY --from=build /go/bin/sql-migrate /usr/local/bin/
COPY --from=build /src/slurpee /usr/local/bin/
COPY --from=build /src/schema /app/schema
COPY --from=build /src/dbconfig.yml /app/dbconfig.yml
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

EXPOSE 8005
ENTRYPOINT ["/app/docker-entrypoint.sh"]
