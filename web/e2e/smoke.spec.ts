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

test("landing demo runs the AI Factory and waits at all three approval gates", async ({ page }) => {
  await page.goto("/");
  await page.getByTestId("hero-demo").click();
  await page.getByTestId("demo-run").click();
  // Gate 1: the plan. The run must not proceed on its own.
  await expect(page.getByTestId("demo-gate")).toContainText(/Approve the plan/i, { timeout: 20_000 });
  await page.getByTestId("demo-approve").click();
  // Gate 2: the exact file changes.
  await expect(page.getByTestId("demo-gate")).toContainText(/Approve these file changes/i, { timeout: 20_000 });
  await page.getByTestId("demo-approve").click();
  // Gate 3: the actual diff, before anything is pushed.
  await expect(page.getByTestId("demo-gate")).toContainText(/Review the code before it's pushed/i, { timeout: 20_000 });
  await expect(page.getByTestId("demo-gate")).toContainText("app/sitemap.ts");
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

test("send back with feedback loops the run to the earlier step, then approve finishes it", async ({ page }) => {
  await signIn(page, ADMIN_EMAIL, ADMIN_PASSWORD);
  // Give the built-in Preview Environments gate a send-back edge to Health Check
  // (restored afterwards so other tests see the shipped workflow).
  const api = async (method: string, path: string, body?: unknown) =>
    page.evaluate(async ({ method, path, body }) => {
      const r = await fetch(path, {
        method,
        headers: { Authorization: "Bearer " + localStorage.getItem("conduit_token"), "X-Conduit-Project": "default", "Content-Type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      return { status: r.status, json: await r.json().catch(() => null) };
    }, { method, path, body });
  const orig = (await api("GET", "/api/workflows/wf_preview_envs")).json;
  const edited = { ...orig, edges: [...orig.edges, { from: "a1", to: "h1", label: "sent_back", max_attempts: 2 }] };
  const put = await api("PUT", "/api/workflows/wf_preview_envs", edited);
  expect(put.status, JSON.stringify(put.json)).toBeLessThan(300);
  try {
    await page.reload();
    await expect(page.getByTestId("run-btn")).toBeVisible();
    await page.getByTestId("run-btn").click();
    await expect(page.getByTestId("approval-banner")).toBeVisible({ timeout: 30_000 });

    // The inspector shows a readable count, not the engine's internal fields.
    const details = page.locator(".inspector .kv");
    await expect(details).toContainText("Send backs left");
    await expect(details).not.toContainText("send back");
    await page.locator(".inspector .insp-tab", { hasText: "Payload" }).click();
    await expect(page.locator(".inspector .code")).not.toContainText("send_back");
    await page.locator(".inspector .insp-tab", { hasText: "Details" }).click();

    await page.getByTestId("send-back-btn").click();
    await expect(page.getByTestId("send-back-dialog")).toContainText("2 send-backs left");
    await expect(page.getByTestId("send-back-confirm")).toBeDisabled(); // feedback is required
    await page.getByTestId("send-back-text").fill("Use the staging region instead");
    await page.getByTestId("send-back-confirm").click();

    // The run loops back, re-runs Health Check, and waits at the gate again —
    // now showing the note and one send-back left.
    await expect(page.getByTestId("approval-banner")).toContainText("Your last feedback: Use the staging region instead", { timeout: 30_000 });
    await page.getByTestId("send-back-btn").click();
    await expect(page.getByTestId("send-back-dialog")).toContainText("last send-back");
    await page.keyboard.press("Escape");

    await page.getByTestId("approve-btn").click();
    await expect(page.getByTestId("approval-banner")).toBeHidden({ timeout: 30_000 });
    await expect(page.getByTestId("run-btn")).toBeVisible({ timeout: 30_000 });
  } finally {
    await api("PUT", "/api/workflows/wf_preview_envs", orig);
  }
});

test("Settings → Project context is saved for the project", async ({ page }) => {
  const email = `e2e_pc_${Date.now()}@example.com`;
  await page.goto("/");
  await page.getByTestId("nav-getstarted").click();
  await page.locator("#name").fill("PC User");
  await page.locator("#email").fill(email);
  await page.locator("#password").fill("password123");
  await page.getByTestId("auth-submit").click();
  await expect(page.getByTestId("run-btn")).toBeVisible();
  await page.getByLabel("Settings").click();
  await page.getByTestId("tab-context").click();
  await page.getByTestId("project-context").fill("Stack: Go 1.24 + Next.js.\nTests: make test.\nOff-limits: billing/.");
  await page.getByTestId("project-context-save").click();
  await expect(page.getByText("Project context saved")).toBeVisible();
  await page.locator(".set-tab", { hasText: "Variables" }).click();
  await expect(page.locator(".secret-list")).toContainText("project_context");
  await page.getByTestId("tab-context").click();
  await expect(page.getByTestId("project-context")).toHaveValue(/Off-limits: billing\//);
});

test("Settings → Models & cost saves the model tiers and step-up setting", async ({ page }) => {
  const email = `e2e_mc_${Date.now()}@example.com`;
  await page.goto("/");
  await page.getByTestId("nav-getstarted").click();
  await page.locator("#name").fill("MC User");
  await page.locator("#email").fill(email);
  await page.locator("#password").fill("password123");
  await page.getByTestId("auth-submit").click();
  await expect(page.getByTestId("run-btn")).toBeVisible();
  await page.getByLabel("Settings").click();
  await page.getByTestId("tab-models").click();
  await expect(page.getByTestId("tier-escalation")).toBeDisabled(); // no tiers yet
  await page.getByTestId("tier-preset").click();
  await expect(page.getByTestId("tier-model_max")).toHaveValue("claude-opus-5-5");
  await expect(page.getByText("$4.00 / $20.00 per M tokens")).toBeVisible();
  await page.getByTestId("tier-escalation").uncheck();
  await page.getByTestId("tier-save").click();
  await expect(page.getByText("Model settings saved")).toBeVisible();
  await page.locator(".set-tab", { hasText: "Variables" }).click();
  for (const k of ["model_fast", "model_standard", "model_max", "model_escalation"]) {
    await expect(page.locator(".secret-list")).toContainText(k);
  }
  await page.getByTestId("tab-models").click();
  await expect(page.getByTestId("tier-model_fast")).toHaveValue("claude-haiku-5-5");
  await expect(page.getByTestId("tier-escalation")).not.toBeChecked();
});

test("diff review: the gate shows the code change, and approving from the viewer continues the run", async ({ page }) => {
  await signIn(page, ADMIN_EMAIL, ADMIN_PASSWORD);
  const api = async (method: string, path: string, body?: unknown) =>
    page.evaluate(async ({ method, path, body }) => {
      const r = await fetch(path, {
        method,
        headers: { Authorization: "Bearer " + localStorage.getItem("conduit_token"), "X-Conduit-Project": "default", "Content-Type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      return { status: r.status, json: await r.json().catch(() => null) };
    }, { method, path, body });
  const diff = [
    "diff --git a/app/config.ts b/app/config.ts", "--- a/app/config.ts", "+++ b/app/config.ts",
    "@@ -1,3 +1,3 @@", " export const config = {", "-  region: \"nyc1\",", "+  region: \"fra1\",", " };",
    "diff --git a/app/health.ts b/app/health.ts", "new file mode 100644", "--- /dev/null", "+++ b/app/health.ts",
    "@@ -0,0 +1,2 @@", "+export const ok = true;", "+export default ok;", "",
  ].join("\n");
  const orig = (await api("GET", "/api/workflows/wf_preview_envs")).json;
  const edited = {
    ...orig,
    nodes: orig.nodes.map((n: { id: string; config: { key: string; value: string }[] }) =>
      n.id === "a1" ? { ...n, config: [...n.config, { key: "diff", icon: "file", value: diff }] } : n),
  };
  expect((await api("PUT", "/api/workflows/wf_preview_envs", edited)).status).toBeLessThan(300);
  try {
    await page.reload();
    await page.getByTestId("run-btn").click();
    await expect(page.getByTestId("approval-banner")).toContainText("review the code before it is pushed", { timeout: 30_000 });
    await page.getByTestId("review-diff-btn").click();
    const viewer = page.getByTestId("diff-review");
    await expect(viewer).toContainText("2 files");
    await expect(viewer).toContainText("+3");
    await expect(viewer).toContainText("−1");
    await expect(page.getByTestId("diff-file")).toHaveCount(2);
    await expect(viewer.locator("tr.del")).toContainText('region: "nyc1"');
    await expect(viewer.locator("tr.add").first()).toContainText('region: "fra1"');
    await expect(viewer).toContainText("new");
    await page.getByTestId("diff-approve").click();
    await expect(viewer).toBeHidden();
    await expect(page.getByTestId("approval-banner")).toBeHidden({ timeout: 30_000 });
    await expect(page.getByTestId("run-btn")).toBeVisible({ timeout: 30_000 });
  } finally {
    await api("PUT", "/api/workflows/wf_preview_envs", orig);
  }
});
