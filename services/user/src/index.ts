import "dotenv/config";
import http2 from "node:http2";

import { connectNodeAdapter } from "@connectrpc/connect-node";

import { registerUserService } from "./grpc/user.handler.js";

const port = Number(process.env.GRPC_PORT ?? 50051);

const handler = connectNodeAdapter({
  routes: (router) => {
    registerUserService(router);
  },
});

http2.createServer(handler).listen(port, () => {
  console.log(`[user-service] gRPC listening on :${port}`);
});
