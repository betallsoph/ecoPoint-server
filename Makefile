.PHONY: up down restart logs ps clean psql mongo \
        setup setup-proto setup-go setup-node \
        gen proto sqlc \
        build test \
        run-user run-gateway run-point run-notification run-reward run-media run-rating run-booking

# ============================================================
# SETUP (chạy 1 lần sau khi clone)
# ============================================================
setup: setup-proto setup-go setup-node
	@echo "✅ Setup xong. Chạy 'make up' để khởi động hạ tầng."

setup-proto:
	@echo "→ Install @bufbuild/protobuf cho generated TS code"
	cd shared/libs/ts && npm install

setup-go:
	@echo "→ go mod download + sqlc generate"
	go mod download
	cd services/point   && sqlc generate
	cd services/booking && sqlc generate
	cd services/reward  && sqlc generate

setup-node:
	@echo "→ Install deps cho 2 Node service"
	cd services/user        && npm install
	cd services/api-gateway && npm install

# ============================================================
# CODE GENERATION
# ============================================================
gen: proto sqlc

proto:
	cd shared/proto && buf format -w && buf lint && buf generate

sqlc:
	cd services/point   && sqlc generate
	cd services/booking && sqlc generate
	cd services/reward  && sqlc generate

# ============================================================
# VERIFY
# ============================================================
build:
	go build ./...
	cd services/user        && npx tsc --noEmit
	cd services/api-gateway && npx tsc --noEmit

test:
	go vet ./...
	cd shared/proto && buf lint

# ============================================================
# DOCKER COMPOSE
# ============================================================
up:
	docker compose --env-file .env up -d

down:
	docker compose down

restart:
	docker compose restart

logs:
	docker compose logs -f

ps:
	docker compose ps

clean:
	docker compose down -v

psql:
	docker exec -it ecopoint-postgres psql -U $${POSTGRES_USER:-ecopoint} -d postgres

mongo:
	docker exec -it ecopoint-mongodb mongosh -u $${MONGO_INITDB_ROOT_USERNAME:-ecopoint} -p $${MONGO_INITDB_ROOT_PASSWORD:-ecopoint_secret}

# ============================================================
# RUN SERVICES (mỗi cái 1 terminal)
# ============================================================
run-user:
	cd services/user && npm run dev

run-gateway:
	cd services/api-gateway && npm run dev

run-point:
	cd services/point && go run ./cmd/server

run-notification:
	cd services/notification && go run ./cmd/server

run-reward:
	cd services/reward && go run ./cmd/server

run-media:
	cd services/media && go run ./cmd/server

run-rating:
	cd services/rating && go run ./cmd/server

run-booking:
	cd services/booking && go run ./cmd/server
