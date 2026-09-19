import "server-only";
import { requireSession } from "./auth";
import { notFound } from "next/navigation";

export async function control<T>(
  path: string,
  method: "GET" | "POST" = "GET",
): Promise<T> {
  await requireSession();
  const token = process.env.HANDOFFGUARD_API_TOKEN;
  const base = new URL(
    process.env.HANDOFFGUARD_SERVER_URL ?? "http://127.0.0.1:8080",
  );
  if (!token || token.length < 32)
    throw new Error("Control API connection is not configured.");
  if (
    base.protocol !== "https:" &&
    !(
      base.protocol === "http:" &&
      ["localhost", "127.0.0.1", "[::1]"].includes(base.hostname)
    )
  )
    throw new Error("Control API requires HTTPS or loopback HTTP.");
  let response: Response;
  try {
    response = await fetch(new URL(path, base), {
      method,
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
      redirect: "error",
      signal: AbortSignal.timeout(15000),
    });
  } catch {
    throw new Error("Cannot reach the Counterseal control API.");
  }
  if (response.status === 404) notFound();
  if (!response.ok)
    throw new Error("The control API could not complete this request.");
  return response.json();
}
export function runID(value: string) {
  if (!/^run_[a-f0-9]{16}$/.test(value)) notFound();
  return value;
}
