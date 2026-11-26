.PHONY: generate lint test test-race build python-lint python-test e2e

generate:
	buf generate
	sqlc generate
	rm -rf clients/python/src/librebots/hub/v1
	cd clients/python && uv run python -m grpc_tools.protoc -I ../../proto --python_out=src --grpc_python_out=src \
		--mypy_out=src --mypy_grpc_out=only_async:src $$(find ../../proto -name '*.proto' | sort)

lint:
	! gofmt -l . | grep .
	go vet ./...
	buf lint
	sqlc diff
	actionlint

test:
	go test ./...

test-race:
	go test -race ./...

build:
	go build -o bin/hub ./cmd/hub

python-lint:
	cd clients/python && uv run ruff check .
	cd clients/python && uv run ruff format --check .
	cd clients/python && uv run pyright

python-test:
	cd clients/python && uv run pytest -m "not e2e"

e2e:
	cd clients/python && uv run pytest -m e2e
