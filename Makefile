.PHONY: up down restart logs ps clean psql mongo

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
