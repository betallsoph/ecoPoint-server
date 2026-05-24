import { ConnectError } from "@connectrpc/connect";
import { GraphQLError } from "graphql";

import { BookingStatus as PbBookingStatus, MaterialType as PbMaterialType } from "@proto/ecopoint/booking/v1/booking_pb.js";
// V1.3: PointSource enum removed — Point Service nhận `source` là string.
import { Timestamp } from "@bufbuild/protobuf";
import { UserRole } from "@proto/ecopoint/user/v1/user_pb.js";

import { auth, ForbiddenError } from "./auth/guard.js";
import { bookingClient, pointClient, rewardClient, userClient } from "./grpc-clients.js";

// ============ helpers ============
const roleToString: Record<UserRole, string> = {
  [UserRole.UNSPECIFIED]: "UNSPECIFIED",
  [UserRole.CUSTOMER]: "CUSTOMER",
  [UserRole.COLLECTOR]: "COLLECTOR",
  [UserRole.ADMIN]: "ADMIN",
};

const bookingStatusToString: Record<PbBookingStatus, string> = {
  [PbBookingStatus.UNSPECIFIED]: "PENDING",
  [PbBookingStatus.PENDING]: "PENDING",
  [PbBookingStatus.ACCEPTED]: "ACCEPTED",
  [PbBookingStatus.COLLECTING]: "COLLECTING",
  [PbBookingStatus.COMPLETED]: "COMPLETED",
  [PbBookingStatus.CANCELLED]: "CANCELLED",
};

const stringToBookingStatus: Record<string, PbBookingStatus> = {
  PENDING: PbBookingStatus.PENDING,
  ACCEPTED: PbBookingStatus.ACCEPTED,
  COLLECTING: PbBookingStatus.COLLECTING,
  COMPLETED: PbBookingStatus.COMPLETED,
  CANCELLED: PbBookingStatus.CANCELLED,
};

const materialToString: Record<PbMaterialType, string> = {
  [PbMaterialType.UNSPECIFIED]: "MIXED",
  [PbMaterialType.PAPER]: "PAPER",
  [PbMaterialType.PLASTIC]: "PLASTIC",
  [PbMaterialType.METAL]: "METAL",
  [PbMaterialType.MIXED]: "MIXED",
};

const stringToMaterial: Record<string, PbMaterialType> = {
  PAPER: PbMaterialType.PAPER,
  PLASTIC: PbMaterialType.PLASTIC,
  METAL: PbMaterialType.METAL,
  MIXED: PbMaterialType.MIXED,
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

function bookingToGraphQL(b: {
  id: string;
  customerId: string;
  collectorId: string;
  status: PbBookingStatus;
  address: string;
  longitude: number;
  latitude: number;
  estimatedKg?: { value: string };
  materialType: PbMaterialType;
  note: string;
  scheduledAt?: Timestamp;
  createdAt?: Timestamp;
  distanceM: number;
}) {
  return {
    id: b.id,
    customerId: b.customerId,
    collectorId: b.collectorId,
    status: bookingStatusToString[b.status] ?? "PENDING",
    address: b.address,
    longitude: b.longitude,
    latitude: b.latitude,
    estimatedKg: b.estimatedKg?.value ?? "0",
    materialType: materialToString[b.materialType] ?? "MIXED",
    note: b.note,
    scheduledAt: b.scheduledAt?.toDate().toISOString() ?? null,
    createdAt: b.createdAt?.toDate().toISOString() ?? null,
    distanceM: b.distanceM,
  };
}

function voucherToGraphQL(v: {
  id: string;
  code: string;
  title: string;
  description: string;
  pointCost?: { value: string };
  stock: number;
  isActive: boolean;
}) {
  return {
    id: v.id,
    code: v.code,
    title: v.title,
    description: v.description,
    pointCost: v.pointCost?.value ?? "0",
    stock: v.stock,
    isActive: v.isActive,
  };
}

function toGraphQLError(err: unknown, fallback = "internal error"): never {
  if (err instanceof ConnectError) {
    const codeMap: Record<number, string> = {
      3: "BAD_REQUEST",
      5: "NOT_FOUND",
      6: "ALREADY_EXISTS",
      7: "FORBIDDEN",
      9: "FAILED_PRECONDITION",
      16: "UNAUTHENTICATED",
    };
    const code = codeMap[err.code] ?? "BAD_REQUEST";
    throw new GraphQLError(err.message, { extensions: { code, connectCode: err.code } });
  }
  console.error("[api-gateway] resolver error:", err);
  throw new GraphQLError(fallback, { extensions: { code: "INTERNAL_SERVER_ERROR" } });
}

// ============ resolvers ============
export const resolvers = {
  Query: {
    me: auth("USER", async (_p, _args, ctx) => {
      try {
        const res = await userClient.getUserInfo({ userId: ctx.user.userId });
        return res.user ? userToGraphQL(res.user) : null;
      } catch (err) {
        toGraphQLError(err, "load profile failed");
      }
    }),

    getUser: auth("USER", async (_p, args: { id: string }, ctx) => {
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

    myBalance: auth("USER", async (_p, _args, ctx) => {
      try {
        const res = await pointClient.getBalance({ userId: ctx.user.userId });
        // V1.3: trả về int64 (BigInt khi qua bufbuild/protobuf). Trả AVAILABLE
        // cho UI; pending hiển thị riêng nếu cần (chưa wire UI).
        return res.balanceAvailable.toString();
      } catch (err) {
        toGraphQLError(err, "get balance failed");
      }
    }),

    myBookings: auth("USER", async (_p, args: { limit?: number }, ctx) => {
      try {
        const res = await bookingClient.listMyBookings({
          customerId: ctx.user.userId,
          limit: args.limit ?? 30,
        });
        return res.bookings.map(bookingToGraphQL);
      } catch (err) {
        toGraphQLError(err, "list my bookings failed");
      }
    }),

    vouchers: auth("USER", async (_p, args: { onlyActive?: boolean }) => {
      try {
        const res = await rewardClient.listVouchers({
          onlyActive: args.onlyActive ?? true,
          limit: 50,
        });
        return res.vouchers.map(voucherToGraphQL);
      } catch (err) {
        toGraphQLError(err, "list vouchers failed");
      }
    }),

    bookings: auth(
      "ADMIN",
      async (_p, args: { status?: string; limit?: number }) => {
        try {
          const statusEnum = args.status
            ? stringToBookingStatus[args.status]
            : PbBookingStatus.UNSPECIFIED;
          const res = await bookingClient.listBookings({
            status: statusEnum ?? PbBookingStatus.UNSPECIFIED,
            limit: args.limit ?? 50,
          });
          return res.bookings.map(bookingToGraphQL);
        } catch (err) {
          toGraphQLError(err, "list bookings failed");
        }
      },
    ),

    pendingNearby: auth(
      "USER",
      async (_p, args: { longitude: number; latitude: number; limit?: number }) => {
        try {
          const res = await bookingClient.listPendingNearby({
            longitude: args.longitude,
            latitude: args.latitude,
            limit: args.limit ?? 5,
          });
          return res.bookings.map(bookingToGraphQL);
        } catch (err) {
          toGraphQLError(err, "knn failed");
        }
      },
    ),
  },

  Mutation: {
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
        const res = await userClient.login({ email: args.email, password: args.password });
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

    createBooking: auth(
      "USER",
      async (
        _p,
        args: {
          input: {
            address: string;
            longitude: number;
            latitude: number;
            estimatedKg: string;
            materialType: string;
            note?: string;
            scheduledAt?: string;
          };
        },
        ctx,
      ) => {
        const i = args.input;
        try {
          const res = await bookingClient.createBooking({
            customerId: ctx.user.userId,
            address: i.address,
            longitude: i.longitude,
            latitude: i.latitude,
            estimatedKg: { value: i.estimatedKg },
            materialType: stringToMaterial[i.materialType] ?? PbMaterialType.MIXED,
            note: i.note ?? "",
            scheduledAt: i.scheduledAt
              ? Timestamp.fromDate(new Date(i.scheduledAt))
              : undefined,
          });
          if (!res.booking) throw new GraphQLError("createBooking missing data");
          return bookingToGraphQL(res.booking);
        } catch (err) {
          toGraphQLError(err, "create booking failed");
        }
      },
    ),

    redeemVoucher: auth(
      "USER",
      async (
        _p,
        args: { voucherId: string; idempotencyKey: string },
        ctx,
      ) => {
        try {
          const res = await rewardClient.redeemVoucher({
            userId: ctx.user.userId,
            voucherId: args.voucherId,
            idempotencyKey: args.idempotencyKey,
          });
          const r = res.redemption;
          return {
            redemption: r
              ? {
                  id: r.id,
                  voucherId: r.voucherId,
                  userId: r.userId,
                  pointCost: r.pointCost?.value ?? "0",
                  status:
                    r.status === 2
                      ? "COMPLETED"
                      : r.status === 3
                        ? "CANCELLED"
                        : "PENDING",
                  pointTxId: r.pointTxId,
                  createdAt: r.createdAt?.toDate().toISOString() ?? null,
                }
              : null,
            newBalance: res.newBalance?.value ?? "0",
          };
        } catch (err) {
          toGraphQLError(err, "redeem failed");
        }
      },
    ),

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
          // V1.3: addPoint mutation giờ tạo PENDING reward (Vựa cần Confirm
          // sau khi xác nhận nhập kho). Amount: string GraphQL → BigInt.
          const res = await pointClient.issuePendingReward({
            userId: args.userId,
            amount: BigInt(args.amount),
            source: "booking_reward",
            referenceId: args.referenceId,
            idempotencyKey: args.idempotencyKey,
          });
          return {
            transactionId: res.transaction?.id ?? "",
            newBalance: res.newBalancePending.toString(),
          };
        } catch (err) {
          toGraphQLError(err, "add point failed");
        }
      },
    ),
  },
};
