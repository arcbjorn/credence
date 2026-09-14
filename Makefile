# Credence.
#
# A thin index over `scripts/`, not a build system: the scripts are the
# real entry points and work without make. This exists so someone meeting
# the repository can type `make` and find out what is available, rather
# than reading ten filenames and guessing which one starts.

.DEFAULT_GOAL := help
.PHONY: help web web-build demos test fmt clean

help: ## Show this list
	@printf '\nCredence — autonomous credit markets for AI agents\n\n'
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[1m%-12s\033[0m %s\n", $$1, $$2}'
	@printf '\nEverything here is a script under scripts/; make is optional.\n\n'

web: ## Set up and serve the interface (one command, from scratch)
	@bash scripts/web.sh

web-build: ## Same, but build the static site instead of serving
	@bash scripts/web.sh --no-serve

demos: ## Run the demos that produce the evidence
	@LOANS=3 bash scripts/demo-repay.sh
	@bash scripts/demo-monitor.sh
	@bash scripts/demo-default.sh

test: ## Every suite, the way CI runs them
	@echo "== contracts ==" && cd contracts && forge fmt --check && forge lint && forge test
	@echo "== engine ==" && cd risk && cargo fmt --check && cargo clippy --all-targets -- -D warnings && cargo test
	@echo "== gateway ==" && cd gateway && go test -race -count=1 ./...
	@echo "== workflow ==" && cd cre && go test -race -count=1 ./underwrite/
	@echo "== docs ==" && bash scripts/check-docs.sh
	@echo "== web ==" && cd web && pnpm test

fmt: ## Format every language (all three are CI gates)
	@cd risk && cargo fmt
	@gofmt -w gateway/ cre/
	@cd contracts && forge fmt
	@echo "formatted rust, go and solidity"

fmt-check: ## Check formatting without rewriting, as CI does
	@cd risk && cargo fmt --check
	@test -z "$$(gofmt -l gateway/ cre/)" || { gofmt -l gateway/ cre/; exit 1; }
	@cd contracts && forge fmt --check
	@echo "formatting matches on all three"

clean: ## Remove generated artifacts and demo evidence
	@rm -rf web/src/engine web/public/data web/dist
	@rm -rf /tmp/credence-repay /tmp/credence-monitor /tmp/credence-default
	@echo "removed generated artifacts; scripts/web.sh rebuilds them"
