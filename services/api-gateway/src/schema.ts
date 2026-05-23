export const typeDefs = /* GraphQL */ `
  enum UserRole {
    UNSPECIFIED
    CUSTOMER
    COLLECTOR
    ADMIN
  }

  enum BookingStatus {
    PENDING
    ACCEPTED
    COLLECTING
    COMPLETED
    CANCELLED
  }

  enum MaterialType {
    PAPER
    PLASTIC
    METAL
    MIXED
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

  type Voucher {
    id: ID!
    code: String!
    title: String!
    description: String!
    pointCost: String!
    stock: Int!
    isActive: Boolean!
  }

  type Redemption {
    id: ID!
    voucherId: ID!
    userId: ID!
    pointCost: String!
    status: String!
    pointTxId: String!
    createdAt: String
  }

  type RedeemResult {
    redemption: Redemption!
    newBalance: String!
  }

  type Booking {
    id: ID!
    customerId: ID!
    collectorId: String!
    status: BookingStatus!
    address: String!
    longitude: Float!
    latitude: Float!
    estimatedKg: String!
    materialType: MaterialType!
    note: String!
    scheduledAt: String
    createdAt: String
    distanceM: Float
  }

  input CreateBookingInput {
    address: String!
    longitude: Float!
    latitude: Float!
    estimatedKg: String!
    materialType: MaterialType!
    note: String
    scheduledAt: String
  }

  # ===== Queries =====
  type Query {
    me: User
    getUser(id: ID!): User

    # Customer
    myBalance: String!
    myBookings(limit: Int): [Booking!]!
    vouchers(onlyActive: Boolean): [Voucher!]!

    # Admin
    bookings(status: BookingStatus, limit: Int): [Booking!]!
    pendingNearby(longitude: Float!, latitude: Float!, limit: Int): [Booking!]!
  }

  # ===== Mutations =====
  type Mutation {
    # Public
    register(
      email: String!
      password: String!
      fullName: String
      phone: String
    ): AuthPayload!
    login(email: String!, password: String!): AuthPayload!

    # Customer (auth)
    createBooking(input: CreateBookingInput!): Booking!
    redeemVoucher(voucherId: ID!, idempotencyKey: String!): RedeemResult!

    # Admin (auth + role)
    addPoint(
      userId: ID!
      amount: String!
      referenceId: String!
      idempotencyKey: String!
    ): AddPointResult!
  }
`;
