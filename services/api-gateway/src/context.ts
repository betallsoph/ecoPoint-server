import type { Request } from "express";

import { UserRole } from "@proto/ecopoint/user/v1/user_pb.js";

import { userClient } from "./grpc-clients.js";

export type Role =
  | "CUSTOMER"
  | "COLLECTOR"
  | "ADMIN"
  | "USER"
  | "STATION_ADMIN"
  | "STATION_STAFF";

export interface AuthUser {
  userId: string;
  email: string;
  role: Role;
}

export interface GraphQLContext {
  user: AuthUser | null;
  authError?: string;
}

const protoRoleToString: Record<UserRole, Role | "UNSPECIFIED"> = {
  [UserRole.UNSPECIFIED]: "UNSPECIFIED",
  [UserRole.CUSTOMER]: "CUSTOMER",
  [UserRole.COLLECTOR]: "COLLECTOR",
  [UserRole.ADMIN]: "ADMIN",
  [UserRole.USER]: "USER",
  [UserRole.STATION_ADMIN]: "STATION_ADMIN",
  [UserRole.STATION_STAFF]: "STATION_STAFF",
};

function extractBearer(header: string | undefined): string | null {
  if (!header) return null;
  const [scheme, token] = header.split(" ");
  if (scheme?.toLowerCase() !== "bearer" || !token) return null;
  return token.trim();
}

/**
 * buildContext chạy 1 lần / request:
 *  - Đọc Authorization: Bearer <token>
 *  - Gọi gRPC ValidateToken → user-service (single source of truth cho JWT)
 *  - Nhét userId/email/role vào context.user, hoặc để null nếu sai/thiếu
 *
 * KHÔNG throw ở đây — query public vẫn chạy. Block đặt ở @auth helper.
 */
export async function buildContext({ req }: { req: Request }): Promise<GraphQLContext> {
  const token = extractBearer(req.headers.authorization);
  if (!token) return { user: null, authError: "missing token" };

  try {
    const res = await userClient.validateToken({ accessToken: token });
    if (!res.valid) {
      return { user: null, authError: "invalid or expired token" };
    }

    const role = protoRoleToString[res.role];
    if (!res.userId || role === "UNSPECIFIED" || !role) {
      return { user: null, authError: "token payload invalid" };
    }

    return {
      user: { userId: res.userId, email: res.email, role },
    };
  } catch (err) {
    // gRPC unavailable / network error → treat as unauthenticated.
    console.error("[api-gateway] validateToken RPC failed:", err);
    return { user: null, authError: "auth service unavailable" };
  }
}
