.PHONY: test fmt vet up down logs

# Single command for the whole test suite: formatting check, vet, and unit tests with the race detector.
test:
	@unformatted=$$(gofmt -l .); if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	go vet ./...
	go test ./... -race -count=1

fmt:
	gofmt -w .

vet:
	go vet ./...

up:
	docker compose up --build -d --wait

down:
	docker compose down -v

logs:
	docker compose logs -f api worker
