"use server";
import { redirect } from "next/navigation";
import { clearSession, createSession, passwordMatches } from "@/lib/auth";
import { control, runID } from "@/lib/api";
import type { Audit } from "@/lib/types";

// Single-operator MVP: bounded process-wide login rate limiting.
let attempts = 0;
let windowStart = 0;
export async function login(_: { error: string }, form: FormData) {
  const now = Date.now();
  if (now - windowStart > 60000) {
    attempts = 0;
    windowStart = now;
  }
  if (++attempts > 10)
    return { error: "Too many attempts. Please wait a minute." };
  const password = form.get("password");
  if (
    typeof password !== "string" ||
    password.length > 1024 ||
    !passwordMatches(password)
  )
    return { error: "Incorrect viewer password." };
  await createSession();
  redirect("/");
}
export async function logout() {
  await clearSession();
  redirect("/login");
}
export async function verifyAudit(id: string): Promise<Audit> {
  return control<Audit>(`/v1/audit/${runID(id)}/verify`, "POST");
}
