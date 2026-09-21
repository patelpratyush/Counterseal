"use server";
import { redirect } from "next/navigation";
import { revalidatePath } from "next/cache";
import { clearSession, loginOperator, requireSession } from "@/lib/auth";
import { ApiError, control, runID } from "@/lib/api";
import type { Audit, Chain } from "@/lib/types";

export async function login(_: { error: string }, form: FormData) {
  const username = form.get("username"), password = form.get("password");
  if (typeof username !== "string" || username.length > 64 || typeof password !== "string" || password.length > 256)
    return { error: "Invalid username or password." };
  const error = await loginOperator(username, password);
  if (error) return { error };
  redirect("/");
}
export async function logout() { await clearSession(); redirect("/login"); }
export async function verifyAudit(id: string): Promise<Audit> {
  return control<Audit>(`/v1/audit/${runID(id)}/verify`, "POST");
}
export async function approveRefund(_: { error: string; success: string }, form: FormData) {
  const { operator } = await requireSession();
  if (operator.role !== "refund_manager") return { error: "A refund manager must approve this request.", success: "" };
  const id = String(form.get("run_id") || ""), envelopeId = String(form.get("envelope_id") || "");
  const raw = String(form.get("amount") || ""), order = String(form.get("order_id") || "");
  const amount = Number(raw);
  if (!/^\d+$/.test(raw) || !Number.isSafeInteger(amount) || amount <= 0 || form.get("confirm") !== "on")
    return { error: "Enter a positive integer amount and confirm the exact refund.", success: "" };
  const chain = await control<Chain>(`/v1/runs/${runID(id)}/chain`);
  const stored = chain.envelopes.find(item => item.envelope.id === envelopeId);
  if (!stored || stored.revoked_at || !stored.envelope.resources.orders?.includes(order))
    return { error: "Choose an active envelope and one of its exact order IDs.", success: "" };
  const expiry = Math.min(Date.now() + 15 * 60 * 1000, Date.parse(stored.envelope.expires_at));
  if (!Number.isFinite(expiry) || expiry <= Date.now()) return { error: "This envelope has expired.", success: "" };
  try {
    await control("/v1/approvals", "POST", {
      envelope_id: envelopeId, action: "refunds.create", resource: `orders:${order}`,
      arguments: { order_id: order, amount }, expires_at: new Date(expiry).toISOString(),
    });
  } catch (error) {
    if (error instanceof ApiError) return { error: error.message, success: "" };
    throw error;
  }
  revalidatePath(`/runs/${id}`);
  return { error: "", success: `Approved by ${operator.display_name}. Resume the prepared workflow to execute this refund.` };
}
