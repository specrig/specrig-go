PROTOCOL ?= ../protocol
COPY := internal/protocol
PLATFORMS := darwin/arm64 darwin/amd64 linux/arm64 linux/amd64 windows/arm64 windows/amd64

test:
	go test ./...
	go -C waffle test ./...

protocol:
	rm -rf $(COPY)/modes.yaml $(COPY)/agents $(COPY)/fixtures
	mkdir -p $(COPY)/agents $(COPY)/fixtures/agents $(COPY)/fixtures/markers $(COPY)/fixtures/records
	cp $(PROTOCOL)/modes.yaml $(COPY)/
	cp $(PROTOCOL)/agents/*.md $(COPY)/agents/
	cp $(PROTOCOL)/fixtures/agents/*.yaml $(COPY)/fixtures/agents/
	cp $(PROTOCOL)/fixtures/markers/*.yaml $(COPY)/fixtures/markers/
	cp $(PROTOCOL)/fixtures/records/* $(COPY)/fixtures/records/
	cp -R $(PROTOCOL)/fixtures/flows $(COPY)/fixtures/

# The binaries a project on any stack assembles its flows with, for the release
# tagged v<version>.
release:
	rm -rf dist
	mkdir dist
	for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; \
		if [ $$os = windows ]; then ext=.exe; fi; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 GOWORK=off \
			go build -trimpath -ldflags=-s -o dist/specrig-flows-$$os-$$arch$$ext ./cmd/specrig-flows || exit 1; \
	done
	cd dist && shasum -a 256 specrig-flows-* >checksums.txt

.PHONY: test protocol release
