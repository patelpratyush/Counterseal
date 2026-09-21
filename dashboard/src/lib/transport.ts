import "server-only";

export async function request(path: string, method = "GET", token?: string, body?: unknown) {
  const base = new URL(process.env.HANDOFFGUARD_SERVER_URL ?? "http://127.0.0.1:8080");
  if (base.username || base.password || base.search || base.hash || (base.pathname !== "/") ||
      (base.protocol !== "https:" && !(base.protocol === "http:" && ["localhost", "127.0.0.1", "[::1]"].includes(base.hostname)))) {
    throw new Error("Control API requires an HTTPS origin or loopback HTTP origin.");
  }
  return fetch(new URL(path, base), {
    method,
    headers: { "Content-Type": "application/json", ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
    cache: "no-store", redirect: "error", signal: AbortSignal.timeout(15000),
  });
}
