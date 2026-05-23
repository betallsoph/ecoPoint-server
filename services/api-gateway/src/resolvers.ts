import { ConnectError } from "@connectrpc/connect";
import { GraphQLError } from "graphql";

import { PointSource } from "@proto/ecopoint/point/v1/point_pb.js";
import { UserRole } from "@proto/ecopoint/user/v1/user_pb.js";

import { auth, ForbiddenError } from "./auth/guard.js";
import { pointClient, userClient } from "./grpc-clients.js";

// ----- helpers -----
const roleToString: Record<UserRole, string> = {
  [UserRole.UNSPECIFIED]: "UNSPECIFIED",
  [UserRole.CUSTOMER]: "CUSTOMER",
  [UserRole.COLLECTOR]: "COLLECTOR",
  [UserRole.ADMIN]: "ADMIN",
};

function userToGraphQL(u: {
  id: string;
  email: string;
  phone: string;
  fullName: string;
  avatarUrl: string;
  role: UserRole;
  isActive: boolean;
}) {
  return {
    id: u.id,
    email: u.email,
    phone: u.phone,
    fullName: u.fullName,
    avatarUrl: u.avatarUrl,
    role: roleToString[u.role] ?? "UNSPECIFIED",
    isActive: u.isActive,
  };
}

// Chuyển ConnectError xuống GraphQLError với code phù hợp.
function toGraphQLError(err: unknown, fallback = "internal error"): never {
  if (err instanceof ConnectError) {
    const code = err.code === 5 ? "NOT_FOUND" : err.code === 6 ? "ALREADY_EXISTS" : "BAD_REQUEST";
    throw new GraphQLError(err.message, {
      extensions: { code, connectCode: err.code },
    });
  }
  console.error("[api-gateway] resolver error:", err);
  throw new GraphQLError(fallback, { extensions: { code: "INTERNAL_SERVER_ERROR" } });
}

export const resolvers = {
  Query: {
    // @auth(role: "USER") — bất kỳ user đã đăng nhập.
    getUser: auth("USER", async (_, args: { id: string }, ctx) => {
      // Bổ sung: non-ADMIN chỉ đọc được profile của chính mình.
      if (ctx.user.role !== "ADMIN" && ctx.user.userId !== args.id) {
        throw new ForbiddenError("can only read your own profile");
      }
      try {
        const res = await userClient.getUserInfo({ userId: args.id });
        return res.user ? userToGraphQL(res.user) : null;
      } catch (err) {
        toGraphQLError(err, "get user failed");
      }
    }),

    me: auth("USER", async (_p, _args, ctx) => {
      try {
        const res = await userClient.getUserInfo({ userId: ctx.user.userId });
        return res.user ? userToGraphQL(res.user) : null;
      } catch (err) {
        toGraphQLError(err, "load profile failed");
      }
    }),
  },

  Mutation: {
    // -------- Public: không cần auth --------
    register: async (
      _p: unknown,
      args: { email: string; password: string; fullName?: string; phone?: string },
    ) => {
      try {
        const res = await userClient.register({
          email: args.email,
          password: args.password,
          fullName: args.fullName ?? "",
          phone: args.phone ?? "",
        });
        if (!res.user) throw new GraphQLError("register response missing user");
        return {
          accessToken: res.accessToken,
          expiresAt: res.expiresAt?.toDate().toISOString() ?? "",
          user: userToGraphQL(res.user),
        };
      } catch (err) {
        toGraphQLError(err, "register failed");
      }
    },

    login: async (_p: unknown, args: { email: string; password: string }) => {
      try {
        const res = await userClient.login({
          email: args.email,
          password: args.password,
        });
        if (!res.user) throw new GraphQLError("login response missing user");
        return {
          accessToken: res.accessToken,
          expiresAt: res.expiresAt?.toDate().toISOString() ?? "",
          user: userToGraphQL(res.user),
        };
      } catch (err) {
        toGraphQLError(err, "login failed");
      }
    },

    // -------- @auth(role: "ADMIN") — chỉ ADMIN cộng điểm --------
    addPoint: auth(
      "ADMIN",
      async (
        _p,
        args: {
          userId: string;
          amount: string;
          referenceId: string;
          idempotencyKey: string;
        },
      ) => {
        try {
          const res = await pointClient.addPoints({
            userId: args.userId,
            amount: { value: args.amount },
            source: PointSource.BOOKING,
            referenceId: args.referenceId,
            idempotencyKey: args.idempotencyKey,
          });
          return {
            transactionId: res.transaction?.id ?? "",
            newBalance: res.newBalance?.value ?? "0",
          };
        } catch (err) {
          toGraphQLError(err, "add point failed");
        }
      },
    ),
  },
};
