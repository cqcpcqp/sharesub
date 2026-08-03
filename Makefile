DEV_SCRIPT := ./scripts/dev.sh

.PHONY: help init check dev status logs stop down restart test

help:
	@$(DEV_SCRIPT) help

init:
	@$(DEV_SCRIPT) init

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

test:
	@$(DEV_SCRIPT) test
