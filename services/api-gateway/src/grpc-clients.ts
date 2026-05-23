import { createPromiseClient } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import { BookingService } from "@proto/ecopoint/booking/v1/booking_connect.js";
import { PointService } from "@proto/ecopoint/point/v1/point_connect.js";
import { RewardService } from "@proto/ecopoint/reward/v1/reward_connect.js";
import { UserService } from "@proto/ecopoint/user/v1/user_connect.js";

function transport(url: string) {
  return createGrpcTransport({ baseUrl: url, httpVersion: "2" });
}

export const userClient = createPromiseClient(
  UserService,
  transport(process.env.USER_SERVICE_URL ?? "http://localhost:50051"),
);

export const pointClient = createPromiseClient(
  PointService,
  transport(process.env.POINT_SERVICE_URL ?? "http://localhost:50053"),
);

export const bookingClient = createPromiseClient(
  BookingService,
  transport(process.env.BOOKING_SERVICE_URL ?? "http://localhost:50052"),
);

export const rewardClient = createPromiseClient(
  RewardService,
  transport(process.env.REWARD_SERVICE_URL ?? "http://localhost:50056"),
);
