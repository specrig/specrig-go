PROTOCOL ?= ../protocol
COPY := internal/protocol

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

.PHONY: test protocol
