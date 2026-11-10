import { test, expect, Page } from "@playwright/test";

// These smoke tests drive the real UI against the real server, guarding the
// end-to-end path that unit tests can't: landing → auth → run → live approval.

const ADMIN_EMAIL = "admin@conduit.local";
const ADMIN_PASSWORD = "conduit";

async function signIn(page: Page, email: string, password: string) {
  await page.goto("/");
  await page.getByTestId("nav-signin").click();
  await page.locator("#email").fill(email);
  await page.locator("#password").fill(password);
  await page.getByTestId("auth-submit").click();
  // The app has loaded once the Run button is visible.
  await expect(page.getByTestId("run-btn")).toBeVisible();
}

test("landing page shows what the app does and offers sign in / create account", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1 })).toContainText(/reviewed pull requests/i);
  await expect(page.getByText(/open-source AI software factory/i).first()).toBeVisible();
  // Real capabilities are described; no fake SSO is advertised.
  await expect(page.getByRole("heading", { name: /Make agents work like they should/i })).toBeVisible();
  await expect(page.getByRole("heading", { name: /How one-shot engineering works/i })).toBeVisible();
  await expect(page.getByText(/when the server has those providers configured/i)).toBeVisible();
  await expect(page.getByTestId("nav-signin")).toBeVisible();
  await expect(page.getByTestId("hero-getstarted")).toBeVisible();
});

test("login → run → approve completes a run (live SSE + approval)", async ({ page }) => {
  await signIn(page, ADMIN_EMAIL, ADMIN_PASSWORD);

  // Start the seeded Preview Environments workflow.
  await page.getByTestId("run-btn").click();

  // The run reaches a human approval gate — this proves SSE events are delivered
  // to the browser (the regression we fixed). The banner must appear.
  await expect(page.getByTestId("approval-banner")).toBeVisible({ timeout: 30_000 });

  // Approve from the banner; the run resumes and finishes, so the banner clears
  // and the Run button returns (no longer showing Cancel).
  await page.getByTestId("approve-btn").click();
  await expect(page.getByTestId("approval-banner")).toBeHidden({ timeout: 30_000 });
  await expect(page.getByTestId("run-btn")).toBeVisible({ timeout: 30_000 });
});

test("create account lands the new user in their own workspace", async ({ page }) => {
  const email = `e2e_${Date.now()}@example.com`;
  await page.goto("/");
  await page.getByTestId("nav-getstarted").click();
  await expect(page.getByTestId("tab-signup")).toHaveAttribute("data-on", "true");
  await page.locator("#name").fill("E2E User");
  await page.locator("#email").fill(email);
  await page.locator("#password").fill("password123");
  await page.getByTestId("auth-submit").click();
  // Signed in: the app canvas is shown.
  await expect(page.getByTestId("run-btn")).toBeVisible();
});
