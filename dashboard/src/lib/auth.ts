import "server-only";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { request } from "./transport";

const cookieName = "cs_operator";
export type Operator = { id: string; username: string; display_name: string; role: "viewer" | "refund_manager" };

export async function requireSession(): Promise<{ operator: Operator; token: string }> {
  const token = (await cookies()).get(cookieName)?.value;
  if (!token || !/^op_[A-Za-z0-9_-]{43}$/.test(token)) redirect("/login");
  const response = await request("/v1/operators/me", "GET", token);
  if (response.status === 401) redirect("/login");
  if (!response.ok) throw new Error("Cannot verify your operator session. Please try again.");
  return { operator: await response.json(), token };
}
export async function loginOperator(username: string, password: string): Promise<string | null> {
  const response = await request("/v1/operators/login", "POST", undefined, { username, password });
  if (response.status === 429) return "Too many attempts. Please wait a minute.";
  if (response.status === 401) return "Invalid username or password.";
  if (!response.ok) return "Sign-in is unavailable. Please try again.";
  const session = await response.json() as { token: string };
  (await cookies()).set(cookieName, session.token, {
    httpOnly: true, secure: process.env.NODE_ENV === "production", sameSite: "strict", path: "/", maxAge: 8 * 60 * 60,
  });
  return null;
}
export async function clearSession() {
  const token = (await cookies()).get(cookieName)?.value;
  if (token) {
    const response = await request("/v1/operators/logout", "POST", token);
    if (!response.ok && response.status !== 401) throw new Error("Sign-out failed. Please try again.");
  }
  (await cookies()).delete(cookieName);
  (await cookies()).delete("hg_viewer");
}
