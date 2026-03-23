BINARY := cymbal
CGO_CFLAGS := -DSQLITE_ENABLE_FTS5

.PHONY: build clean test install lint

build:
	CGO_CFLAGS="$(CGO_CFLAGS)" go build -o $(BINARY) .

install:
	CGO_CFLAGS="$(CGO_CFLAGS)" go install .

test:
	CGO_CFLAGS="$(CGO_CFLAGS)" go test ./...

lint:
	go vet ./...

clean:
	rm -f $(BINARY)
