

%.css.gz: %.css
	gzip -k -9 -f $<
	touch $<.gz

%.js.gz: %.js
	gzip -k -9 -f $<
	touch $<.gz

%.css.zst: %.css
	zstd -q -19 -k -f $<
	touch $<.zst

%.js.zst: %.js
	zstd -q -19 -k -f $<
	touch $<.zst

SQL_FILES = $(wildcard queries/*.sql)
SCHEMA_FILES = $(wildcard schema/*.sql)
STATIC_JS = $(wildcard static/*.js)
STATIC_CSS = $(wildcard static/*.css)
COMP_STATIC_JS = $(STATIC_JS:.js=.js.gz) $(STATIC_JS:.js=.js.zst)
COMP_STATIC_CSS = $(STATIC_CSS:.css=.css.gz) $(STATIC_CSS:.css=.css.zst)

GO_FILES = $(shell find . -name '*.go')
TEMPL_FILES = $(shell find . -name '*.templ')

all: slurpee

db/models.go: $(SQL_FILES) $(SCHEMA_FILES)
	go tool sqlc generate

slurpee: $(COMP_STATIC_JS) $(COMP_STATIC_CSS) $(GO_FILES) $(TEMPL_FILES) db/models.go
	go build

slurpee-dev: slurpee
	./slurpee --dev


dev: slurpee
	go tool templ generate --watch --proxy="http://localhost:8000" -proxyport 8001 --cmd="make slurpee-dev"

