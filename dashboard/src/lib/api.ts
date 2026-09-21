import "server-only";
import { requireSession } from "./auth";
import { request } from "./transport";
import { notFound, redirect } from "next/navigation";

export class ApiError extends Error {}
export async function control<T>(path: string, method: "GET" | "POST" = "GET", body?: unknown): Promise<T> {
  const { token } = await requireSession();
  const response = await request(path, method, token, body);
  if (response.status === 401) redirect("/login");
  if (response.status === 404) notFound();
  if (!response.ok) {
    if ([400, 403, 409].includes(response.status)) {
      const data = await response.json() as { error?: string };
      throw new ApiError(data.error || "The request was rejected.");
    }
    throw new Error("The control API could not complete this request.");
  }
  return response.json();
}
export function runID(value: string) {
  if (!/^run_[a-f0-9]{16}$/.test(value)) notFound();
  return value;
}
