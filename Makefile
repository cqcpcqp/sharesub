DEV_SCRIPT := ./scripts/dev.sh

.PHONY: help init install-hooks check dev status logs stop down restart quality quality-structure test

help:
	@$(DEV_SCRIPT) help

init:
	@$(DEV_SCRIPT) init

install-hooks:
	@git config core.hooksPath .githooks
	@echo "Git hooks enabled from .githooks"

check:
	@$(DEV_SCRIPT) check

dev:
	@$(DEV_SCRIPT) dev

status:
	@$(DEV_SCRIPT) status

logs:
	@$(DEV_SCRIPT) logs

stop:
	@$(DEV_SCRIPT) stop

down:
	@$(DEV_SCRIPT) down

restart:
	@$(DEV_SCRIPT) restart

quality-structure:
	@./scripts/check-quality-structure.sh

quality: quality-structure
	@./scripts/quality.sh

test:
	@./scripts/verify-version.sh >/dev/null
	@./scripts/verify-version-test.sh
	@./scripts/deploy-test.sh
	@$(DEV_SCRIPT) test
