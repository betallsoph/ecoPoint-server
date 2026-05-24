import { GraphQLError, type GraphQLResolveInfo } from "graphql";

import type { AuthUser, GraphQLContext, Role } from "../context.js";

// ----- GraphQL errors chuẩn -----
export class UnauthorizedError extends GraphQLError {
  constructor(reason = "Unauthorized") {
    super(reason, {
      extensions: { code: "UNAUTHENTICATED", http: { status: 401 } },
    });
  }
}

export class ForbiddenError extends GraphQLError {
  constructor(reason = "Forbidden") {
    super(reason, {
      extensions: { code: "FORBIDDEN", http: { status: 403 } },
    });
  }
}

export type AuthedContext = GraphQLContext & { user: AuthUser };

type Resolver<TArgs, TResult, TCtx> = (
  parent: unknown,
  args: TArgs,
  ctx: TCtx,
  info: GraphQLResolveInfo,
) => TResult | Promise<TResult>;

// ──────────────────────────────────────────────────────────────
// RBAC spec — chấp nhận macro string hoặc Role[] tường minh.
// ──────────────────────────────────────────────────────────────
export type RoleSpec = "USER" | "ADMIN" | "STATION" | Role[];

const macroToRoles: Record<Exclude<RoleSpec, Role[]>, Role[]> = {
  // USER = bất kỳ user đã đăng nhập (mọi role hợp lệ).
  USER: ["CUSTOMER", "COLLECTOR", "ADMIN", "USER", "STATION_ADMIN", "STATION_STAFF"],
  // ADMIN = duy nhất platform admin.
  ADMIN: ["ADMIN"],
  // STATION = chủ Vựa hoặc nhân viên Vựa.
  STATION: ["STATION_ADMIN", "STATION_STAFF"],
};

function allowedRoles(spec: RoleSpec): Role[] {
  return Array.isArray(spec) ? spec : macroToRoles[spec];
}

/**
 * @auth — RBAC guard. Chặn anonymous + check role.
 *
 *   auth("USER",    fn)           → mọi user authenticated.
 *   auth("ADMIN",   fn)           → chỉ ADMIN.
 *   auth("STATION", fn)           → STATION_ADMIN | STATION_STAFF.
 *   auth(["ADMIN", "STATION_ADMIN"], fn)  → list tường minh.
 */
export function auth<TArgs = unknown, TResult = unknown>(
  spec: RoleSpec,
  resolver: Resolver<TArgs, TResult, AuthedContext>,
): Resolver<TArgs, TResult, GraphQLContext> {
  const allowed = allowedRoles(spec);
  return (parent, args, ctx, info) => {
    if (!ctx.user) {
      throw new UnauthorizedError(ctx.authError ?? "Unauthorized");
    }
    if (!allowed.includes(ctx.user.role)) {
      throw new ForbiddenError(
        `Forbidden: role ${ctx.user.role} not in [${allowed.join(", ")}]`,
      );
    }
    return resolver(parent, args, ctx as AuthedContext, info);
  };
}
