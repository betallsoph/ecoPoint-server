import { createPromiseClient } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import { UserService } from "@proto/ecopoint/user/v1/user_connect.js";
import { PointService } from "@proto/ecopoint/point/v1/point_connect.js";

const userTransport = createGrpcTransport({
  baseUrl: process.env.USER_SERVICE_URL ?? "http://localhost:50051",
  httpVersion: "2",
});

const pointTransport = createGrpcTransport({
  baseUrl: process.env.POINT_SERVICE_URL ?? "http://localhost:50053",
  httpVersion: "2",
});

export const userClient = createPromiseClient(UserService, userTransport);
export const pointClient = createPromiseClient(PointService, pointTransport);
