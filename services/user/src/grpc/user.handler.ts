import { Timestamp } from "@bufbuild/protobuf";
import { Code, ConnectError, type ConnectRouter } from "@connectrpc/connect";
import { eq, sql } from "drizzle-orm";

import { UserService } from "@proto/ecopoint/user/v1/user_connect.js";
import { UserRole } from "@proto/ecopoint/user/v1/user_pb.js";

import { type Role, signAccessToken, TokenError, verifyAccessToken } from "../auth/jwt.js";
import { comparePassword, hashPassword } from "../auth/password.js";
import { db, schema } from "../db/index.js";
import type { UserRow } from "../db/schema.js";

// ----- Mapping -----
const dbRoleToProto: Record<UserRow["role"], UserRole> = {
  customer: UserRole.CUSTOMER,
  collector: UserRole.COLLECTOR,
  admin: UserRole.ADMIN,
};
const dbRoleToToken: Record<UserRow["role"], Role> = {
  customer: "CUSTOMER",
  collector: "COLLECTOR",
  admin: "ADMIN",
};
const tokenRoleToProto: Record<Role, UserRole> = {
  CUSTOMER: UserRole.CUSTOMER,
  COLLECTOR: UserRole.COLLECTOR,
  ADMIN: UserRole.ADMIN,
};

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const PG_UNIQUE_VIOLATION = "23505";

function toProtoUser(row: UserRow) {
  return {
    id: row.id,
    email: row.email,
    phone: row.phone ?? "",
    fullName: row.fullName ?? "",
    avatarUrl: row.avatarUrl ?? "",
    role: dbRoleToProto[row.role] ?? UserRole.UNSPECIFIED,
    isActive: row.isActive,
    createdAt: Timestamp.fromDate(row.createdAt),
    updatedAt: Timestamp.fromDate(row.updatedAt),
  };
}

function isPgError(err: unknown, code: string): boolean {
  return (
    typeof err === "object" &&
    err !== null &&
    "code" in err &&
    (err as { code: unknown }).code === code
  );
}

export const registerUserService = (router: ConnectRouter) =>
  router.service(UserService, {
    // -------------------- Register --------------------
    async register(req) {
      const email = req.email.trim().toLowerCase();
      const password = req.password;
      const fullName = req.fullName.trim() || null;
      const phone = req.phone.trim() || null;

      if (!EMAIL_RE.test(email)) {
        throw new ConnectError("invalid email format", Code.InvalidArgument);
      }
      if (password.length < 8) {
        throw new ConnectError("password must be at least 8 characters", Code.InvalidArgument);
      }

      try {
        const passwordHash = await hashPassword(password);
        const [row] = await db
          .insert(schema.users)
          .values({ email, passwordHash, fullName, phone })
          .returning();

        if (!row) {
          throw new ConnectError("failed to create user", Code.Internal);
        }

        const { token, expiresAt } = signAccessToken({
          userId: row.id,
          email: row.email,
          role: dbRoleToToken[row.role],
        });

        return {
          accessToken: token,
          expiresAt: Timestamp.fromDate(expiresAt),
          user: toProtoUser(row),
        };
      } catch (err) {
        if (err instanceof ConnectError) throw err;
        if (isPgError(err, PG_UNIQUE_VIOLATION)) {
          throw new ConnectError("email already registered", Code.AlreadyExists);
        }
        // Không leak stack/internal — log để debug nhưng trả message gọn.
        console.error("[user-service] register failed:", err);
        throw new ConnectError("registration failed", Code.Internal);
      }
    },

    // -------------------- Login --------------------
    async login(req) {
      const email = req.email.trim().toLowerCase();
      const password = req.password;

      if (!email || !password) {
        throw new ConnectError("email and password are required", Code.InvalidArgument);
      }

      try {
        const [row] = await db
          .select()
          .from(schema.users)
          .where(eq(schema.users.email, email))
          .limit(1);

        // Trả CÙNG MỘT lỗi cho user-not-found và sai password
        // → tránh attacker enumerate email.
        if (!row) {
          throw new ConnectError("invalid credentials", Code.Unauthenticated);
        }
        if (!row.isActive) {
          throw new ConnectError("account disabled", Code.PermissionDenied);
        }
        const ok = await comparePassword(password, row.passwordHash);
        if (!ok) {
          throw new ConnectError("invalid credentials", Code.Unauthenticated);
        }

        const { token, expiresAt } = signAccessToken({
          userId: row.id,
          email: row.email,
          role: dbRoleToToken[row.role],
        });

        return {
          accessToken: token,
          expiresAt: Timestamp.fromDate(expiresAt),
          user: toProtoUser(row),
        };
      } catch (err) {
        if (err instanceof ConnectError) throw err;
        console.error("[user-service] login failed:", err);
        throw new ConnectError("login failed", Code.Internal);
      }
    },

    // -------------------- ValidateToken --------------------
    async validateToken(req) {
      const token = req.accessToken?.trim();
      if (!token) {
        return {
          valid: false,
          userId: "",
          email: "",
          role: UserRole.UNSPECIFIED,
        };
      }
      try {
        const { payload, expiresAt } = verifyAccessToken(token);
        return {
          valid: true,
          userId: payload.userId,
          email: payload.email,
          role: tokenRoleToProto[payload.role],
          expiresAt: Timestamp.fromDate(expiresAt),
        };
      } catch (err) {
        // Không tiết lộ chi tiết lý do thất bại — chỉ valid=false.
        if (!(err instanceof TokenError)) {
          console.error("[user-service] validateToken unexpected error:", err);
        }
        return {
          valid: false,
          userId: "",
          email: "",
          role: UserRole.UNSPECIFIED,
        };
      }
    },

    // -------------------- GetUserInfo --------------------
    async getUserInfo({ userId }) {
      if (!userId) {
        throw new ConnectError("user_id is required", Code.InvalidArgument);
      }
      try {
        const [row] = await db
          .select()
          .from(schema.users)
          .where(eq(schema.users.id, userId))
          .limit(1);
        if (!row) {
          throw new ConnectError(`user not found: ${userId}`, Code.NotFound);
        }
        return { user: toProtoUser(row) };
      } catch (err) {
        if (err instanceof ConnectError) throw err;
        console.error("[user-service] getUserInfo failed:", err);
        throw new ConnectError("internal error", Code.Internal);
      }
    },

    // -------------------- DeductTrustScore --------------------
    async deductTrustScore({ userId, amount, reason }) {
      if (!userId) {
        throw new ConnectError("user_id is required", Code.InvalidArgument);
      }
      if (amount <= 0) {
        throw new ConnectError("amount must be positive", Code.InvalidArgument);
      }
      try {
        // Atomic UPDATE với GREATEST(0, …) — DB floor 0 luôn, không cần read-modify-write.
        const [row] = await db
          .update(schema.users)
          .set({
            trustScore: sql`GREATEST(0, ${schema.users.trustScore} - ${amount})`,
            updatedAt: new Date(),
          })
          .where(eq(schema.users.id, userId))
          .returning({ trustScore: schema.users.trustScore });
        if (!row) {
          throw new ConnectError(`user not found: ${userId}`, Code.NotFound);
        }
        console.warn("[user-service] trust score deducted", {
          userId,
          amount,
          reason,
          newTrustScore: row.trustScore,
        });
        return { newTrustScore: row.trustScore };
      } catch (err) {
        if (err instanceof ConnectError) throw err;
        console.error("[user-service] deductTrustScore failed:", err);
        throw new ConnectError("deductTrustScore failed", Code.Internal);
      }
    },
  });
