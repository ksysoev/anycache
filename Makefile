test:
	docker compose run --rm tests

bench:
	go test ./tests -run '^$$' -bench . -benchmem

lint:
	golangci-lint run

proto:
	protoc --go_out=. --go_opt=paths=source_relative \
		storage/memcache/proto/cached_item.proto
