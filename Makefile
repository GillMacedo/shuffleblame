.PHONY: test lint demo

RUN = go run ./plugin/scripts

test:
	test -z "$$(gofmt -l plugin tests examples)"
	go vet ./...
	cd examples/shop && go vet ./...
	go test ./...

lint:
	claude plugin validate ./plugin --strict
	claude plugin validate . --strict

# pricing has order-dependent tests on purpose, so it exits 1 with -fail.
demo:
	$(RUN) examples/shop/pricing
	$(RUN) -log examples/logs/shuffle-4.txt examples/shop/pricing
	$(RUN) -fail examples/shop/ledger
