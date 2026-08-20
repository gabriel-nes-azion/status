BIN := status

.PHONY: build run test race vet lint clean

build:
	go build -o $(BIN) .

run: build
	./$(BIN)

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

# Everything CI should gate on.
lint: vet
	gofmt -l . | tee /dev/stderr | (! read)

clean:
	rm -f $(BIN)
