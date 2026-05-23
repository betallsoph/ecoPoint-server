import { ApolloServer } from "@apollo/server";
import { expressMiddleware } from "@as-integrations/express5";
import cors from "cors";
import express from "express";

import { resolvers } from "./resolvers.js";
import { typeDefs } from "./schema.js";

async function main() {
  const app = express();
  const apollo = new ApolloServer({ typeDefs, resolvers });
  await apollo.start();

  app.use(
    "/graphql",
    cors(),
    express.json(),
    expressMiddleware(apollo),
  );

  app.get("/healthz", (_req, res) => res.json({ ok: true }));

  const port = Number(process.env.PORT ?? 4000);
  app.listen(port, () => {
    console.log(`[api-gateway] GraphQL ready → http://localhost:${port}/graphql`);
  });
}

main().catch((err) => {
  console.error("[api-gateway] fatal:", err);
  process.exit(1);
});
