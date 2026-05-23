import { PointSource } from "@proto/ecopoint/point/v1/point_pb.js";
import { UserRole } from "@proto/ecopoint/user/v1/user_pb.js";

import { pointClient, userClient } from "./grpc-clients.js";

const roleToString: Record<UserRole, string> = {
  [UserRole.UNSPECIFIED]: "UNSPECIFIED",
  [UserRole.CUSTOMER]: "CUSTOMER",
  [UserRole.COLLECTOR]: "COLLECTOR",
  [UserRole.ADMIN]: "ADMIN",
};

export const resolvers = {
  Query: {
    getUser: async (_: unknown, args: { id: string }) => {
      const res = await userClient.getUserInfo({ userId: args.id });
      const u = res.user;
      if (!u) return null;
      return {
        id: u.id,
        email: u.email,
        phone: u.phone,
        fullName: u.fullName,
        avatarUrl: u.avatarUrl,
        role: roleToString[u.role] ?? "UNSPECIFIED",
        isActive: u.isActive,
      };
    },
  },

  Mutation: {
    addPoint: async (
      _: unknown,
      args: {
        userId: string;
        amount: string;
        referenceId: string;
        idempotencyKey: string;
      },
    ) => {
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
    },
  },
};
