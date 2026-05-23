#!/usr/bin/env bash
# =============================================================
# Ecopoint :: bootstrap PostgreSQL databases
# Tự động chạy bởi image postgres khi container init lần đầu.
# Đọc biến POSTGRES_MULTIPLE_DATABASES (CSV) và tạo từng DB.
# =============================================================
set -euo pipefail

if [[ -z "${POSTGRES_MULTIPLE_DATABASES:-}" ]]; then
  echo "[ecopoint] POSTGRES_MULTIPLE_DATABASES is empty, skip."
  exit 0
fi

create_db() {
  local db="$1"
  echo "[ecopoint] Creating database: ${db}"
  psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "postgres" <<-EOSQL
    SELECT 'CREATE DATABASE ${db}'
    WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '${db}')\gexec
    GRANT ALL PRIVILEGES ON DATABASE ${db} TO ${POSTGRES_USER};
EOSQL
}

enable_ext() {
  local db="$1"; shift
  for ext in "$@"; do
    psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$db" \
      -c "CREATE EXTENSION IF NOT EXISTS \"${ext}\";"
  done
}

IFS=',' read -ra DBS <<< "$POSTGRES_MULTIPLE_DATABASES"
for db in "${DBS[@]}"; do
  db="$(echo "$db" | xargs)"
  [[ -z "$db" ]] && continue
  create_db "$db"
  enable_ext "$db" "uuid-ossp" "pgcrypto"
done

# Riêng db_booking cần PostGIS cho dữ liệu địa lý điểm thu gom
if [[ ",$POSTGRES_MULTIPLE_DATABASES," == *",db_booking,"* ]]; then
  echo "[ecopoint] Enabling PostGIS on db_booking"
  enable_ext "db_booking" "postgis" "postgis_topology"
fi

echo "[ecopoint] Database bootstrap done."
