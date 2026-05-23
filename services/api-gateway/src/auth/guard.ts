import { GraphQLError, type GraphQLResolveInfo } from "graphql";

import type { AuthUser, GraphQLContext } from "../context.js";

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

/**
 * @auth — RBAC guard.
 *
 *   - `auth("ADMIN", resolver)` → chỉ user có role ADMIN.
 *   - `auth("USER",  resolver)` → mọi user đã đăng nhập (CUSTOMER, COLLECTOR, ADMIN).
 *
 * Hành vi:
 *   - ctx.user == null               → throw 401 Unauthorized
 *   - role yêu cầu ADMIN mà user khác → throw 403 Forbidden
 *   - hợp lệ                          → resolver chạy với ctx narrow `AuthedContext`.
 */
export function auth<TArgs = unknown, TResult = unknown>(
  role: "ADMIN" | "USER",
  resolver: Resolver<TArgs, TResult, AuthedContext>,
): Resolver<TArgs, TResult, GraphQLContext> {
  return (parent, args, ctx, info) => {
    if (!ctx.user) {
      throw new UnauthorizedError(ctx.authError ?? "Unauthorized");
    }
    if (role === "ADMIN" && ctx.user.role !== "ADMIN") {
      throw new ForbiddenError(`Forbidden: ADMIN only (have ${ctx.user.role})`);
    }
    // role === "USER" → any authenticated user passes.
    return resolver(parent, args, ctx as AuthedContext, info);
  };
}
