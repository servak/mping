VERSION = 2.0
GIT_HASH := $(shell git describe --always)

mping:
	go build -ldflags "-s \
    -X main.Version=v$(VERSION) \
    -X main.Revision=$(GIT_HASH)" ./cmd/mping

build: mping

clean:
	rm -f mping
