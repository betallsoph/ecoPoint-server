export const typeDefs = /* GraphQL */ `
  enum UserRole {
    UNSPECIFIED
    CUSTOMER
    COLLECTOR
    ADMIN
  }

  type User {
    id: ID!
    email: String!
    phone: String!
    fullName: String!
    avatarUrl: String!
    role: UserRole!
    isActive: Boolean!
  }

  type AuthPayload {
    accessToken: String!
    expiresAt: String!
    user: User!
  }

  type AddPointResult {
    transactionId: ID!
    newBalance: String!
  }

  # ===== Queries =====
  type Query {
    # Trả profile (cần đăng nhập).
    getUser(id: ID!): User
    # Profile của chính user đang login.
    me: User
  }

  # ===== Mutations =====
  type Mutation {
    # ----- Public auth -----
    register(
      email: String!
      password: String!
      fullName: String
      phone: String
    ): AuthPayload!

    login(email: String!, password: String!): AuthPayload!

    # ----- Authenticated -----
    addPoint(
      userId: ID!
      amount: String!
      referenceId: String!
      idempotencyKey: String!
    ): AddPointResult!
  }
`;
