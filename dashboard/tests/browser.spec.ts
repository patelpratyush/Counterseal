import { expect, test, type APIRequestContext, type Page } from "@playwright/test";

interface Envelope {
  id?: string;
  signature?: unknown;
  issuer: { agent: string };
  recipient: { agent: string };
  allowed_actions: string[];
  delegation: { max_depth: number; current_depth: number; may_expand_authority: boolean };
  [field: string]: unknown;
}
interface Issued { envelope: Envelope; run_id: string }

async function seed(request: APIRequestContext) {
  const post = (path: string, data: unknown) => request.post(`${process.env.HANDOFFGUARD_SERVER_URL}${path}`, {
    data, headers: { Authorization: `Bearer ${process.env.HANDOFFGUARD_API_TOKEN}` },
  });
  const envelope: Envelope = {
    version: "1", issuer: { agent: "demo-operator" }, recipient: { agent: "support-agent" },
    purpose: "customer_refund", policy_version: "refund-v1",
    allowed_actions: ["orders.read", "refunds.create", "email.send"], denied_actions: ["payments.export"],
    resources: { orders: ["48319"] }, data_classes: ["payment_metadata"],
    approvals: [{ condition: "has(refund.amount) && refund.amount > 500", required_role: "refund_manager" }],
    delegation: { max_depth: 2, current_depth: 0, may_expand_authority: false },
    expires_at: new Date(Date.now() + 3_600_000).toISOString(),
  };
  const response = await post("/v1/envelopes", { envelope });
  expect(response.status()).toBe(201);
  const root = await response.json() as Issued;
  let parent = root.envelope;
  for (const [stage, actions] of [["billing-agent", ["refunds.create", "email.send"]], ["notification-agent", ["email.send"]]] as const) {
    const child = structuredClone(parent);
    delete child.id;
    delete child.signature;
    child.issuer = parent.recipient;
    child.recipient = { agent: stage };
    child.parent_envelope = parent.id;
    child.allowed_actions = [...actions];
    child.delegation.current_depth += 1;
    const delegated = await post(`/v1/envelopes/${parent.id}/delegate`, { child });
    expect(delegated.status()).toBe(201);
    if (stage === "billing-agent") {
      const bad = structuredClone(child);
      bad.allowed_actions.push("payments.export");
      expect((await post(`/v1/envelopes/${parent.id}/delegate`, { child: bad })).status()).toBe(403);
    }
    parent = (await delegated.json() as Issued).envelope;
  }
  return root.run_id;
}

async function settle(page: Page) {
  // Speculative RSC streams can remain open after the page is ready.
  await page.waitForLoadState("networkidle", { timeout: 3000 }).catch(() => page.waitForLoadState("domcontentloaded"));
  await page.evaluate(() => document.fonts.ready);
}

test("real API: authentication, search, delegation, audit, theme, mobile, logout", async ({ page, request }) => {
  const run = await seed(request);
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.goto(`/runs/${run}`);
  await expect(page).toHaveURL(/\/login$/);
  await page.getByLabel("Viewer password").fill("wrong-password");
  await page.getByRole("button", { name: "Open console" }).click();
  await expect(page.locator(".login-form [role=alert]")).toContainText("Incorrect viewer password");
  await page.getByLabel("Viewer password").fill(process.env.HANDOFFGUARD_DASHBOARD_PASSWORD!);
  await page.getByRole("button", { name: "Open console" }).click();
  await expect(page.getByRole("heading", { name: "Runs", exact: true })).toBeVisible();
  await settle(page);
  expect(await page.content()).not.toContain(process.env.HANDOFFGUARD_API_TOKEN!);
  await page.screenshot({ path: "/tmp/handoffguard-overview.png", fullPage: true });
  await page.getByRole("link", { name: "Blocked runs", exact: true }).click();
  await expect(page).toHaveURL(/filter=blocked/);
  await page.getByLabel("Search runs").fill("not-a-real-run");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await expect(page.getByRole("heading", { name: "No matching runs" })).toBeVisible();
  await page.goto(`/runs/${run}`);
  await settle(page);
  await expect(page.getByText("Authority map", { exact: true })).toBeVisible();
  await expect(page.locator(".react-flow__node")).toHaveCount(4);
  await page.getByRole("button", { name: "Handoff 2 · DENY", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Delegation blocked" })).toBeVisible();
  await expect(page.locator(".violations")).toContainText("ACTION_EXPANDED");
  await page.getByRole("button", { name: "Handoff 1 · ALLOW", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Authority inherited" })).toBeVisible();
  await page.getByRole("button", { name: "Verify audit", exact: true }).click();
  await expect(page.getByText("Audit valid", { exact: true })).toBeVisible();
  await page.screenshot({ path: "/tmp/handoffguard-run.png", fullPage: true });
  await page.getByRole("tab", { name: "Decision history" }).click();
  await expect(
    page.getByRole("tabpanel", { name: /Decision history/ })
      .getByRole("heading", { name: "Delegation denied", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Toggle color theme" }).click();
  await expect(page.locator("html")).toHaveClass(/dark/);
  await page.getByRole("tab", { name: "Delegation graph" }).click();
  await page.setViewportSize({ width: 390, height: 844 });
  await settle(page);
  await page.screenshot({ path: "/tmp/handoffguard-mobile.png", fullPage: true });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  const canvas = (await page.locator(".graph-canvas").boundingBox())!;
  for (const node of await page.locator(".react-flow__node").all()) {
    const bounds = (await node.boundingBox())!;
    expect(bounds.x).toBeGreaterThanOrEqual(canvas.x - 1);
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(canvas.x + canvas.width + 1);
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);
  await page.goto(`/runs/${run}`);
  await expect(page).toHaveURL(/\/login$/);
  expect(errors).toEqual([]);
});
