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

  type AddPointResult {
    transactionId: ID!
    newBalance: String!
  }

  type Query {
    getUser(id: ID!): User
  }

  type Mutation {
    addPoint(
      userId: ID!
      amount: String!
      referenceId: String!
      idempotencyKey: String!
    ): AddPointResult!
  }
`;
