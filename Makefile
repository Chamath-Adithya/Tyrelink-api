dev:
	go run ./backend/cmd/api

db-up:
	docker compose up -d postgres

db-down:
	docker compose down

backend-test:
	cd backend && go test ./...

frontend-build:
	npm run build
