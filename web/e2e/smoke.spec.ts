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

test("landing page says what the app does and offers sign in / create account", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1 })).toContainText(/Tickets in\.\s*Reviewed pull requests out\./i);
  await expect(page.getByText(/turns a ticket into a pull request/i)).toBeVisible();
  // Every major section is present.
  for (const name of [
    /Watch a ticket become a pull request/i, /Three things to set up/i, /Six agent steps/i,
    /Plugs into the tools your team already ships with/i, /Two workflows ship ready/i,
    /See where work is/i, /Self-hosted\. Yours to run/i,
  ]) {
    await expect(page.getByRole("heading", { name })).toBeAttached();
  }
  // The page must not drift back into claims the product does not back.
  const body = page.locator("main");
  for (const banned of [/Salesforce/i, /HubSpot/i, /lead scor/i, /No coding required/i, /Talk to our team/i, /per month/i]) {
    await expect(body).not.toContainText(banned);
  }
  // No fake SSO is advertised, and sample figures are labelled as such.
  await expect(page.getByText(/when the server has those\s+providers configured/i)).toBeAttached();
  await expect(page.getByText(/shown on this page are sample data/i)).toBeAttached();
  await expect(page.getByTestId("nav-signin")).toBeVisible();
  await expect(page.getByTestId("hero-getstarted")).toBeVisible();
});

test("landing demo runs the AI Factory and waits at both approval gates", async ({ page }) => {
  await page.goto("/");
  await page.getByTestId("hero-demo").click();
  await page.getByTestId("demo-run").click();
  // Gate 1: the plan. The run must not proceed on its own.
  await expect(page.getByTestId("demo-gate")).toContainText(/Approve the plan/i, { timeout: 20_000 });
  await page.getByTestId("demo-approve").click();
  // Gate 2: the exact file changes.
  await expect(page.getByTestId("demo-gate")).toContainText(/Approve these file changes/i, { timeout: 20_000 });
  await page.getByTestId("demo-approve").click();
  await expect(page.getByTestId("demo-outcome")).toContainText(/Pull request #129 opened/i, { timeout: 20_000 });
  await expect(page.locator(".cv-tl")).toContainText("Verify checks (CI)");
  await expect(page.getByTestId("demo-run")).toHaveText(/Run again/i);
});

test("landing demo routes a vague ticket to a person and stops on rejection", async ({ page }) => {
  await page.goto("/");
  await page.getByTestId("hero-demo").click();
  await page.getByTestId("demo-ticket-vague").click();
  await page.getByTestId("demo-run").click();
  await expect(page.getByTestId("demo-outcome")).toContainText(/Needs clarification/i, { timeout: 20_000 });
  await expect(page.locator(".cv-tl")).not.toContainText("Refine & plan");

  await page.getByTestId("demo-ticket-clear").click();
  await page.getByTestId("demo-run").click();
  await page.getByTestId("demo-reject").click({ timeout: 20_000 });
  await expect(page.getByTestId("demo-outcome")).toContainText(/Needs attention/i);
  await expect(page.locator(".cv-tl")).not.toContainText("Apply changes");
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
  // Signed in, in their own copy of the AI Factory (not an empty workspace).
  await expect(page.getByTestId("run-btn")).toBeVisible();
  await expect(page.locator(".project-switch option:checked")).toHaveText("E2E User · AI Factory");
  await expect(page.getByLabel("Workflow name")).toHaveValue(/AI Factory/);
  await expect(page.locator(".project-switch option")).toContainText(["E2E User · AI Factory", "E2E User · GitHub"]);
});

test("project create/delete use in-app dialogs, never a browser prompt or confirm", async ({ page }) => {
  let native = 0;
  page.on("dialog", (d) => { native++; d.dismiss(); });
  await signIn(page, ADMIN_EMAIL, ADMIN_PASSWORD);
  const name = `E2E ${Date.now()}`;

  // "+ New project…" opens the in-app prompt; cancelling creates nothing.
  await page.locator(".project-switch").selectOption("__new__");
  await expect(page.getByTestId("prompt-dialog")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByTestId("prompt-dialog")).toBeHidden();

  await page.locator(".project-switch").selectOption("__new__");
  await page.getByTestId("dialog-input").fill(name);
  await page.getByTestId("dialog-confirm").click();
  await expect(page.locator(".project-switch option:checked")).toHaveText(name);

  // Deleting asks in an in-app confirm; Cancel keeps the project, Confirm removes it.
  await page.getByLabel("Delete this project").click();
  await expect(page.getByTestId("confirm-dialog")).toContainText(name);
  await page.getByTestId("dialog-cancel").click();
  await expect(page.locator(".project-switch option:checked")).toHaveText(name);

  await page.getByLabel("Delete this project").click();
  await page.getByTestId("dialog-confirm").click();
  await expect(page.locator(".project-switch")).not.toContainText(name);
  expect(native).toBe(0);
});

test("Settings → Coding agent switches the project to Claude Code and keeps the API option", async ({ page }) => {
  const email = `e2e_ca_${Date.now()}@example.com`;
  await page.goto("/");
  await page.getByTestId("nav-getstarted").click();
  await page.locator("#name").fill("CA User");
  await page.locator("#email").fill(email);
  await page.locator("#password").fill("password123");
  await page.getByTestId("auth-submit").click();
  await expect(page.getByTestId("run-btn")).toBeVisible();

  await page.getByLabel("Settings").click();
  // Existing projects stay on the direct API until someone opts in.
  await expect(page.getByTestId("engine-api")).toHaveAttribute("aria-checked", "true");
  await page.getByTestId("engine-claude").click();
  await page.getByTestId("claude-token").fill("sk-ant-oat01-e2e");
  await page.getByTestId("claude-commands").fill("npm test");
  await page.getByTestId("coding-save").click();
  await expect(page.getByText("Coding agent saved")).toBeVisible();

  await page.locator(".set-tab", { hasText: "Variables" }).click();
  const vars = page.locator(".secret-list");
  await expect(vars).toContainText("coding_engine");
  await expect(vars).toContainText("claude_code");
  await expect(vars).toContainText("npm test");
  await page.locator(".set-tab", { hasText: "Secrets" }).click();
  await expect(page.locator(".secret-list")).toContainText("claude_code_oauth_token");
  await expect(page.locator(".secret-list")).not.toContainText("sk-ant-oat01-e2e");

  // And back to the API engine.
  await page.getByTestId("tab-coding").click();
  await page.getByTestId("engine-api").click();
  await page.getByTestId("coding-save").click();
  await page.locator(".set-tab", { hasText: "Variables" }).click();
  await expect(page.locator(".secret-list")).toContainText(/coding_engine\s*api/);
});
