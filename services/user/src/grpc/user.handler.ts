import { Timestamp } from "@bufbuild/protobuf";
import type { ConnectRouter } from "@connectrpc/connect";
import { eq } from "drizzle-orm";

import { UserService } from "@proto/ecopoint/user/v1/user_connect.js";
import { UserRole } from "@proto/ecopoint/user/v1/user_pb.js";

import { db, schema } from "../db/index.js";
import type { UserRow } from "../db/schema.js";

const dbRoleToProto: Record<UserRow["role"], UserRole> = {
  customer: UserRole.CUSTOMER,
  collector: UserRole.COLLECTOR,
  admin: UserRole.ADMIN,
};

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

export const registerUserService = (router: ConnectRouter) =>
  router.service(UserService, {
    async getUserInfo({ userId }) {
      const [row] = await db
        .select()
        .from(schema.users)
        .where(eq(schema.users.id, userId))
        .limit(1);

      if (!row) {
        throw new Error(`user_not_found: ${userId}`);
      }
      return { user: toProtoUser(row) };
    },

    async verifyToken({ accessToken }) {
      // TODO: tích hợp JWT verify thật ở giai đoạn sau.
      const valid = !!accessToken && accessToken.length > 10;
      return {
        valid,
        userId: valid ? "stub-user-id" : "",
        role: UserRole.UNSPECIFIED,
        expiresAt: Timestamp.fromDate(new Date(Date.now() + 3600_000)),
      };
    },
  });
