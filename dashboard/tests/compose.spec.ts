import { expect, test } from "@playwright/test";

test("container dashboard displays and verifies a Java workflow", async ({ page }) => {
  const password = process.env.HG_COMPOSE_PASSWORD;
  const run = process.env.HG_COMPOSE_RUN_ID;
  if (!password || !run) throw new Error("Run this test through scripts/test-compose.sh");
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.goto(`/runs/${run}`);
  await expect(page).toHaveURL(/\/login$/);
  await page.getByLabel("Viewer password").fill(password);
  await page.getByRole("button", { name: "Open console" }).click();
  await expect(page.getByRole("heading", { name: "Runs", exact: true })).toBeVisible();
  await page.goto(`/runs/${run}`);
  await expect(page.locator(".react-flow__node")).toHaveCount(3);
  await page.getByRole("button", { name: "Handoff 1 · ALLOW", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Authority inherited" })).toBeVisible();
  await page.getByRole("button", { name: "Verify audit", exact: true }).click();
  await expect(page.getByText("Audit valid", { exact: true })).toBeVisible();
  await page.screenshot({ path: "/tmp/handoffguard-compose.png", fullPage: true });
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);
  expect(errors).toEqual([]);
});
