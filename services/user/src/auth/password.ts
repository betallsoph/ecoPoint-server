import bcrypt from "bcryptjs";

// Salt rounds 10 — cân bằng tốc độ & độ an toàn cho HS-level password.
// Tăng lên 12 nếu CPU dư dả; KHÔNG hạ dưới 10.
const SALT_ROUNDS = 10;

export async function hashPassword(plain: string): Promise<string> {
  return bcrypt.hash(plain, SALT_ROUNDS);
}

export async function comparePassword(plain: string, hash: string): Promise<boolean> {
  return bcrypt.compare(plain, hash);
}
