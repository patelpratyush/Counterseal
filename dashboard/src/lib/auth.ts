import "server-only";
import {
  createHmac,
  createHash,
  timingSafeEqual,
  randomBytes,
} from "node:crypto";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";

const cookieName = "hg_viewer";
const duration = 8 * 60 * 60;
function config() {
  const secret = process.env.HANDOFFGUARD_DASHBOARD_SECRET ?? "";
  const password = process.env.HANDOFFGUARD_DASHBOARD_PASSWORD ?? "";
  if (secret.length < 32 || password.length < 16)
    throw new Error(
      "Configure the dashboard password (16+ characters) and session secret (32+ characters).",
    );
  return { secret, password };
}
function signature(payload: string) {
  const { secret, password } = config();
  return createHmac("sha256", secret)
    .update(password)
    .update("\0")
    .update(payload)
    .digest("base64url");
}
function equal(a: string, b: string) {
  return timingSafeEqual(
    createHash("sha256").update(a).digest(),
    createHash("sha256").update(b).digest(),
  );
}
export function passwordMatches(value: string) {
  return equal(value, config().password);
}
export async function hasSession() {
  const value = (await cookies()).get(cookieName)?.value;
  if (!value || value.length > 512) return false;
  const [payload, mac, extra] = value.split(".");
  if (!payload || !mac || extra || !equal(signature(payload), mac))
    return false;
  try {
    const { expires } = JSON.parse(
      Buffer.from(payload, "base64url").toString(),
    );
    return (
      typeof expires === "number" &&
      expires > Date.now() &&
      expires <= Date.now() + duration * 1000
    );
  } catch {
    return false;
  }
}
export async function requireSession() {
  if (!(await hasSession())) redirect("/login");
}
export async function createSession() {
  const payload = Buffer.from(
    JSON.stringify({
      expires: Date.now() + duration * 1000,
      nonce: randomBytes(16).toString("hex"),
    }),
  ).toString("base64url");
  (await cookies()).set(cookieName, payload + "." + signature(payload), {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "strict",
    path: "/",
    maxAge: duration,
  });
}
export async function clearSession() {
  (await cookies()).delete(cookieName);
}
