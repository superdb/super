export GO111MODULE=on

VERSION = $(shell git describe --tags --dirty --always)
LDFLAGS = -s -X github.com/superdb/super/cli.version=$(VERSION)
BUILD_COMMANDS = ./cmd/super

ifeq ($(GO),$(shell command -v go 2>/dev/null))
  $(error Go is not installed or not in PATH)
endif

ifeq "$(filter-out 386 arm mips mipsle, $(shell go env GOARCH))" ""
$(error 32-bit architectures are unsupported; see https://github.com/superdb/super/issues/4044)
endif

# This enables a shortcut to run a single test from the ./ztests suite, e.g.:
#  make TEST=TestSPQ/ztests/suite/cut/cut
ifneq "$(TEST)" ""
test-one: test-run
endif

# Uncomment this to trigger re-builds of the peg files when the grammar
# is out of date.  We are commenting this out to work around issue #1717.
#PEG_DEP=peg

vet:
	go vet ./...
	$(MAKE) -C book $@

fmt:
	gofmt -s -w .
	git diff --exit-code -- '*.go'

tidy:
	go mod tidy
	git diff --exit-code -- go.mod go.sum

generate:
	go generate ./...

test-generate: generate
	git diff --exit-code

test-unit:
	@go test -short ./...

test-system: build bin/rustfs
	@ZTEST_PATH="$(CURDIR)/dist:$(CURDIR)/bin" go test .

test-boomerangs: build bin/rustfs
	@ZTEST_PATH="$(CURDIR)/dist:$(CURDIR)/bin" go test -parallel 1 . -run TestSPQ/boomerang -v

test-run: build bin/rustfs
	@ZTEST_PATH="$(CURDIR)/dist:$(CURDIR)/bin" go test . -v -run $(TEST)

test-heavy: build
	@PATH="$(CURDIR)/dist:$(PATH)" go test -tags=heavy ./mdtest

bin/rustfs: Makefile
	arch=$$(uname -m | sed -e s/^arm/aarch/) \
	os=$$(uname -s | sed -e s/^Darwin/macos/ | tr A-Z a-z) && \
	  [ $$os != linux ] || libc=-musl && \
	  url=https://github.com/rustfs/rustfs/releases/download/1.0.0-rc.5/rustfs-$${os}-$${arch}$${libc}-latest.zip && \
	  echo $$url && \
	  curl -Lf --create-dirs -o $@.zip $$url
	unzip -oq $@.zip -d $(@D)
	touch -c $@

build: $(PEG_DEP)
	@mkdir -p dist
	@go build -ldflags='$(LDFLAGS)' -o dist ./cmd/...

install:
	@go install -ldflags='$(LDFLAGS)' $(BUILD_COMMANDS)

.PHONY: installdev
installdev:
	@go install -ldflags='$(LDFLAGS)' ./cmd/...

compiler/parser/parser.go: compiler/parser/parser.peg go.mod
	go tool pigeon -support-left-recursion -o $@ $<
	go tool goimports -w $@

.PHONY: peg
peg: compiler/parser/parser.go

.PHONY: markdown-lint
markdown-lint:
	@npm install --no-save markdownlint-cli@0.35.0
	@npx markdownlint --ignore-path .gitignore .

# CI performs these actions individually since that looks nicer in the UI;
# this is a shortcut so that a local dev can easily run everything.
test-ci: fmt tidy vet test-generate test-unit test-system test-heavy
	$(MAKE) -C book codespell

clean:
	@rm -rf dist

.PHONY: fmt tidy vet test-unit test-system test-heavy test-ci
.PHONY: build install clean generate test-generate
