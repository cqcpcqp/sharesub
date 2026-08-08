DEV_SCRIPT := ./scripts/dev.sh
DEPLOY_SCRIPT := ./scripts/deploy.sh

.PHONY: help init check dev status logs stop down restart test deploy deploy-status deploy-logs deploy-backup

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
	@./scripts/verify-version.sh >/dev/null
	@./scripts/verify-version-test.sh
	@$(DEV_SCRIPT) test

deploy:
	@$(DEPLOY_SCRIPT) deploy

deploy-status:
	@$(DEPLOY_SCRIPT) status

deploy-logs:
	@$(DEPLOY_SCRIPT) logs

deploy-backup:
	@$(DEPLOY_SCRIPT) backup
