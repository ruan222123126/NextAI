.PHONY: dev gateway cli-test cli-coverage gateway-test gateway-coverage contract-test test

GO_COVERAGE_THRESHOLD := 45

dev:
	@echo "Start gateway: make gateway"

gateway:
	cd apps/gateway && go run ./cmd/gateway

cli-test:
	cd apps/cli && pnpm test

cli-coverage:
	cd apps/cli && pnpm run test:coverage

gateway-test:
	cd apps/gateway && go test ./...

gateway-coverage:
	cd apps/gateway && go test ./... -coverprofile=coverage.out -coverpkg=./...
	cd apps/gateway && \
		total=$$(go tool cover -func=coverage.out | awk '/^total:/ {gsub("%","",$$3); print $$3}'); \
		echo "Go coverage total: $${total}% (threshold: $(GO_COVERAGE_THRESHOLD)%)"; \
		awk -v total="$$total" -v threshold="$(GO_COVERAGE_THRESHOLD)" 'BEGIN { if (total+0 < threshold+0) { printf("Go coverage %.1f%% below threshold %.1f%%\n", total+0, threshold+0); exit 1 } }'

contract-test:
	cd tests/contract && pnpm test

test: gateway-test cli-test contract-test
