import jwt, { type Algorithm, type JwtPayload, type SignOptions } from "jsonwebtoken";

export type Role = "CUSTOMER" | "COLLECTOR" | "ADMIN";

export interface TokenPayload {
  userId: string;
  email: string;
  role: Role;
}

export interface SignResult {
  token: string;
  expiresAt: Date;
}

export class TokenError extends Error {
  constructor(
    public readonly reason: "missing" | "malformed" | "expired" | "invalid",
    message?: string,
  ) {
    super(message ?? reason);
    this.name = "TokenError";
  }
}

// -------- Config (đọc env 1 lần, fail-fast nếu thiếu) --------
const SECRET = process.env.JWT_SECRET ?? "";
const ALGORITHM = (process.env.JWT_ALGORITHM ?? "HS256") as Algorithm;
const ISSUER = process.env.JWT_ISSUER || "ecopoint";
const EXPIRES_IN = (process.env.JWT_EXPIRES_IN ?? "7d") as SignOptions["expiresIn"];

if (!SECRET || SECRET.length < 32) {
  throw new Error("JWT_SECRET is required and must be at least 32 chars");
}

const ROLES: readonly Role[] = ["CUSTOMER", "COLLECTOR", "ADMIN"];

function isRole(v: unknown): v is Role {
  return typeof v === "string" && (ROLES as readonly string[]).includes(v);
}

// ---------- Sign ----------
export function signAccessToken(payload: TokenPayload): SignResult {
  const token = jwt.sign(
    { userId: payload.userId, email: payload.email, role: payload.role },
    SECRET,
    {
      algorithm: ALGORITHM,
      issuer: ISSUER,
      subject: payload.userId,
      expiresIn: EXPIRES_IN,
    },
  );

  const decoded = jwt.decode(token) as JwtPayload | null;
  const expiresAt =
    decoded?.exp !== undefined ? new Date(decoded.exp * 1000) : new Date(Date.now() + 7 * 24 * 3600 * 1000);
  return { token, expiresAt };
}

// ---------- Verify ----------
export function verifyAccessToken(token: string): { payload: TokenPayload; expiresAt: Date } {
  let decoded: JwtPayload;
  try {
    const result = jwt.verify(token, SECRET, {
      algorithms: [ALGORITHM], // chốt allowlist → chặn `none`/RS/HS confusion.
      issuer: ISSUER,
    });
    if (typeof result === "string") {
      throw new TokenError("malformed", "token payload is a string");
    }
    decoded = result;
  } catch (err) {
    if (err instanceof jwt.TokenExpiredError) throw new TokenError("expired");
    if (err instanceof jwt.JsonWebTokenError) throw new TokenError("invalid", err.message);
    throw err;
  }

  const userId =
    typeof decoded.userId === "string"
      ? decoded.userId
      : typeof decoded.sub === "string"
        ? decoded.sub
        : undefined;
  const email = typeof decoded.email === "string" ? decoded.email : undefined;
  const role = decoded.role;

  if (!userId || !email || !isRole(role)) {
    throw new TokenError("malformed", "missing or invalid claims");
  }

  const expiresAt = decoded.exp ? new Date(decoded.exp * 1000) : new Date();
  return { payload: { userId, email, role }, expiresAt };
}
