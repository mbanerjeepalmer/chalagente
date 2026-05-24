import { test, expect } from "@playwright/test";

test("admin requires basic auth", async ({ baseURL }) => {
  const res = await fetch(`${baseURL}/admin`);
  expect(res.status).toBe(401);
});

test("admin shows mode toggle, defaults to scripted, can switch to agent", async ({ page }) => {
  await page.goto("/admin");

  await expect(page.getByRole("heading", { name: "Reply mode" })).toBeVisible();
  await expect(page.locator("p", { hasText: "Current:" })).toContainText("scripted");

  const scriptedBtn = page.getByRole("button", { name: "Use scripted" });
  const agentBtn = page.getByRole("button", { name: "Use agent" });

  await expect(scriptedBtn).toBeDisabled();
  await expect(agentBtn).toBeEnabled();

  await agentBtn.click();

  await expect(page.locator(".flash")).toContainText("mode set to agent");
  await expect(page.locator("p", { hasText: "Current:" })).toContainText("agent");
  await expect(page.getByRole("button", { name: "Use agent" })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Use scripted" })).toBeEnabled();

  // Restore default for any other tests.
  await page.getByRole("button", { name: "Use scripted" }).click();
  await expect(page.locator("p", { hasText: "Current:" })).toContainText("scripted");
});
