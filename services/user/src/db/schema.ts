import {
  boolean,
  integer,
  pgEnum,
  pgTable,
  text,
  timestamp,
  uuid,
  varchar,
} from "drizzle-orm/pg-core";

export const userRoleEnum = pgEnum("user_role", [
  "customer",
  "collector",
  "admin",
]);

export const users = pgTable("users", {
  id: uuid("id").primaryKey().defaultRandom(),
  email: varchar("email", { length: 255 }).notNull().unique(),
  passwordHash: varchar("password_hash", { length: 255 }).notNull(),
  role: userRoleEnum("role").notNull().default("customer"),

  phone: varchar("phone", { length: 32 }),
  fullName: varchar("full_name", { length: 255 }),
  avatarUrl: text("avatar_url"),
  isActive: boolean("is_active").notNull().default(true),

  // V1.3 Trust Layer — bị trừ khi gian lận. Floor 0 enforce ở app + DB CHECK.
  trustScore: integer("trust_score").notNull().default(100),

  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  updatedAt: timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
});

export type UserRow = typeof users.$inferSelect;
export type NewUser = typeof users.$inferInsert;
