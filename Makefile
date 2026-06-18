.PHONY: help up down logs server test test-metrics health metrics loadtest-normal loadtest-duplicate loadtest-mixed-signatures loadtest-spike

help:
	@printf '%s\n' \
		'Available targets:' \
		'  make up                         Start the full Docker Compose stack' \
		'  make down                       Stop the Docker Compose stack' \
		'  make logs                       Tail application logs from Docker Compose' \
		'  make server                     Run the Go server directly' \
		'  make test                       Run all Go tests' \
		'  make test-metrics               Run metrics and docs asset tests' \
		'  make health                     Check the local health endpoint' \
		'  make metrics                    Fetch the local metrics endpoint' \
		'  make loadtest-normal            Run the normal k6 scenario' \
		'  make loadtest-duplicate         Run the duplicate k6 scenario' \
		'  make loadtest-mixed-signatures  Run the mixed signature k6 scenario' \
		'  make loadtest-spike             Run the spike k6 scenario'

up:
	docker compose up --build

down:
	docker compose down

logs:
	docker compose logs -f app

server:
	go run ./cmd/server

test:
	go test ./...

test-metrics:
	go test ./internal/metrics

health:
	curl -fsS http://localhost:8080/healthz

metrics:
	curl -fsS http://localhost:8080/metrics

loadtest-normal:
	k6 run -e SCENARIO=normal loadtest/payment_webhooks.js

loadtest-duplicate:
	k6 run -e SCENARIO=duplicate loadtest/payment_webhooks.js

loadtest-mixed-signatures:
	k6 run -e SCENARIO=mixed_signatures loadtest/payment_webhooks.js

loadtest-spike:
	k6 run -e SCENARIO=spike loadtest/payment_webhooks.js
