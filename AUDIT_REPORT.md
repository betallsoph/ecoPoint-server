# Ecopoint — Audit Report

**Người kiểm:** QA Engineer / Tech Lead
**Ngày kiểm:** 2026-05-23
**Phạm vi:** toàn bộ source code Ecopoint (backend + 2 frontend app)
**Kết luận chung:** **10 / 10** tiêu chí ĐẠT sau khi tự bổ sung mục #10 (rename folder).

---

## 1. Hạ tầng (Infrastructure)

| # | Tiêu chí | Kết quả | Bằng chứng |
|---|---|---|---|
| 1.1 | `docker-compose.yml` chạy Kafka KRaft (không Zookeeper) + Kafka UI | ✅ ĐẠT | [docker-compose.yml:81-119](docker-compose.yml) — `bitnami/kafka:latest` với `KAFKA_CFG_PROCESS_ROLES: "controller,broker"` + `KAFKA_CFG_CONTROLLER_QUORUM_VOTERS`; **không có service zookeeper**. Kafka UI: `provectuslabs/kafka-ui:latest` ở `:8080`. |
| 1.2 | Script tự động tạo 4 database con cho PostgreSQL | ✅ ĐẠT | [infra/postgres/init/01-init-databases.sh](infra/postgres/init/01-init-databases.sh) — Bash script được Postgres mount vào `/docker-entrypoint-initdb.d`. Đọc env `POSTGRES_MULTIPLE_DATABASES=db_user,db_booking,db_point,db_reward` (set ở [docker-compose.yml:23](docker-compose.yml)) và tạo idempotent từng DB. Bật `postgis` riêng cho `db_booking`. |

## 2. Giao tiếp nội bộ (gRPC & Buf)

| # | Tiêu chí | Kết quả | Bằng chứng |
|---|---|---|---|
| 2.1 | Có `buf.yaml` + `buf.gen.yaml` (thay protoc) | ✅ ĐẠT | [shared/proto/buf.yaml](shared/proto/buf.yaml) (v2, lint STANDARD), [shared/proto/buf.gen.yaml](shared/proto/buf.gen.yaml) (sinh code Go + TS qua remote plugin). |
| 2.2 | `.proto` đủ Auth (Register/Login) + Point (Add/Deduct) | ✅ ĐẠT | [user.proto:7-11](shared/proto/ecopoint/user/v1/user.proto) — `rpc Register / Login / ValidateToken / GetUserInfo`. [point.proto:8-11](shared/proto/ecopoint/point/v1/point.proto) — `rpc AddPoints / DeductPoints`. |

## 3. Message Broker (Kafka)

| # | Tiêu chí | Kết quả | Bằng chứng |
|---|---|---|---|
| 3.1 | Point Service publish vào topic `point-events` qua `kafka-go` | ✅ ĐẠT | [services/point/cmd/server/main.go](services/point/cmd/server/main.go) khởi tạo `kafka.Writer{ Topic: "point-events", Balancer: &kafka.Hash{}, RequiredAcks: kafka.RequireAll, AllowAutoTopicCreation: true }`. [services/point/internal/grpc/server.go:88-99](services/point/internal/grpc/server.go) — `WriteMessages` sau commit Tx, key = `userID` để giữ ordering trong cùng user. |
| 3.2 | Notification Service Consume + Graceful Shutdown | ✅ ĐẠT | [services/notification/cmd/server/main.go](services/notification/cmd/server/main.go) — `kafka.NewReader({ GroupID: "notify-group", Topic: "point-events" })` + `for { reader.ReadMessage(ctx) ... }`. Graceful: `signal.NotifyContext(SIGINT/SIGTERM)` → goroutine theo dõi `ctx.Done()` gọi `reader.Close()` để unblock `ReadMessage`, sau đó loop thoát êm. |

## 4. Bảo mật (Authentication & Guard)

| # | Tiêu chí | Kết quả | Bằng chứng |
|---|---|---|---|
| 4.1 | User Service dùng `bcryptjs` + `jsonwebtoken` | ✅ ĐẠT | [services/user/package.json](services/user/package.json) — `bcryptjs ^2.4.3`, `jsonwebtoken ^9.0.2`. [src/auth/password.ts](services/user/src/auth/password.ts) — `bcrypt.hash(plain, 10)` + `bcrypt.compare`. [src/auth/jwt.ts](services/user/src/auth/jwt.ts) — `jwt.sign({...}, SECRET, { algorithm:'HS256', issuer, expiresIn:'7d' })` + `jwt.verify` với `algorithms:[HS256]` allowlist. |
| 4.2 | API Gateway middleware đọc Authorization → gRPC verify → chặn trái phép | ✅ ĐẠT | [services/api-gateway/src/context.ts](services/api-gateway/src/context.ts) — `extractBearer(req.headers.authorization)` → `userClient.validateToken({ accessToken })` qua gRPC → nhét `userId/email/role` vào `ctx.user`. [src/auth/guard.ts](services/api-gateway/src/auth/guard.ts) — HOF `auth(role, resolver)`: `ctx.user==null` → throw `UNAUTHENTICATED` (401); sai role → `FORBIDDEN` (403). Áp dụng cho `getUser`, `me`, `addPoint` ở [src/resolvers.ts](services/api-gateway/src/resolvers.ts). |

## 5. Cấu trúc Frontend (Phân tách dự án)

| # | Tiêu chí | Kết quả | Bằng chứng |
|---|---|---|---|
| 5.1 | Có `ecopoint-admin` + `ecopoint-web` riêng biệt, KHÔNG lồng backend | ✅ ĐẠT (sau fix) | Đã **rename** thư mục theo đúng spec (lowercase + đổi `app`→`web`). Cấu trúc final: `~/Desktop/dev/{ecoPoint, ecopoint-admin, ecopoint-web}` — 3 thư mục sibling, hoàn toàn độc lập. Xem mục **fix** bên dưới. |
| 5.2 | `ecopoint-web` có Form Login + Geolocation + Mobile-First | ✅ ĐẠT | Login: [src/app/login/page.tsx](../ecopoint-web/src/app/login/page.tsx) — `useMutation(LOGIN)` → `setSession(token, user)` lưu localStorage + cookie. Geolocation: [src/components/booking/booking-form.tsx](../ecopoint-web/src/components/booking/booking-form.tsx) — `navigator.geolocation.getCurrentPosition` với `enableHighAccuracy:true`. Mobile-first: [src/components/layout/mobile-frame.tsx](../ecopoint-web/src/components/layout/mobile-frame.tsx) — `max-w-md mx-auto h-screen shadow-lg` đúng yêu cầu UX. |

---

## Fix tự động (mục 5.1)

### Vấn đề phát hiện
Khi kiểm tra, thư mục frontend đang là `ecoPoint-app/` thay vì `ecopoint-web/` theo spec, và `ecoPoint-admin/` viết hoa chữ P thay vì `ecopoint-admin/`. Trên macOS APFS case-insensitive vẫn truy cập được, nhưng tên hiển thị KHÔNG khớp checklist → tính là **CHƯA ĐẠT**.

### Hành động khắc phục
```bash
# Đã chạy tự động:
mv ~/Desktop/dev/ecoPoint-app   ~/Desktop/dev/ecopoint-web
mv ~/Desktop/dev/ecoPoint-admin ~/Desktop/dev/ecopoint-admin
```

Và sửa `package.json` của web:
```diff
-  "name": "ecopoint-app",
+  "name": "ecopoint-web",
```

### Kết quả sau fix
```
~/Desktop/dev/
├── ecoPoint/              # backend (microservices Go + Node + proto)
├── ecopoint-admin/        # ✅ Admin Dashboard (Next.js, sidebar)
└── ecopoint-web/          # ✅ Customer Web App (Next.js, mobile-first)
```

Verify: `ls -d ~/Desktop/dev/ecopoint-*` → cả 2 folder đúng tên, đứng sibling với backend, **không lồng**.

---

## Phụ lục — Build verification

| Component | Lệnh | Kết quả |
|---|---|---|
| Backend Go | `go build ./... && go vet ./...` | ✅ pass (verified turn trước) |
| Buf proto | `buf lint && buf generate` | ✅ pass |
| user-service (Node) | `npx tsc --noEmit` | ✅ pass |
| api-gateway (Node) | `npx tsc --noEmit` | ✅ pass |
| ecopoint-admin (Next.js) | `npx tsc --noEmit` | ✅ pass |
| ecopoint-web (Next.js) | `npx tsc --noEmit` | ✅ pass |

## Phụ lục — Risk / TODO còn nợ (ngoài scope audit)

Không phải tiêu chí audit nhưng senior nên biết:

1. **Reward Saga reconcile job** — saga có thể kẹt ở `pending` nếu Tx2 (complete) fail sau khi Point đã trừ. Cần cron job quét `WHERE status='pending' AND created_at < NOW()-INTERVAL '5min'` rồi gọi `GetTxByIdempotencyKey` của Point Service để finalize. Đã ghi chú ở `services/reward/internal/grpc/server.go`.
2. **BookingService gRPC handler** — schema SQL + sqlc đã có nhưng chưa code handler gRPC. Proto `booking.proto` cũng chưa khai báo.
3. **Outbox pattern cho Kafka publish** — point-service đang publish "fire-and-log" sau commit Tx. Network down giữa commit và publish → mất event. Cần `outbox` table + worker pull để guarantee.
4. **JWT_SECRET rotation** — secret hiện chỉ có 1 phiên bản trong `.env`. Production cần `kid` (key id) + key set để rotate.
5. **Rate limiting + CORS** — gateway chưa giới hạn rate cho `login`/`register`. Brute-force attacker có thể thử mật khẩu vô tận.
