// Run scan button visibility based on the authenticated user's groups.
//
// The button is only rendered when `/api/me` returns `canRunScan: true`. This
// spec exercises that frontend gate by mocking the `/api/me` response — no real
// OIDC issuer involved. Server-side enforcement (the bearer → userinfo →
// admin-group check) is covered by internal/dashboard/auth_test.go and
// internal/dashboard/internal_server_test.go.

const { test, expect } = require('@playwright/test');
const { BASE, shellReady, mockMe } = require('./helpers');

const ADMIN = {
  authEnabled: true,
  email: 'admin@example.com',
  groups: ['provenance-admins'],
  canRunScan: true,
  features: {},
};

const runScan = (page) => page.getByRole('button', { name: /Run scan|Scan running/ });

test.describe('Run scan button visibility', () => {
  test('absent when auth is disabled', async ({ page }) => {
    await mockMe(page, { authEnabled: false, canRunScan: false, features: {} });
    await page.goto(BASE);
    await shellReady(page);
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Overview');
    await expect(runScan(page)).toHaveCount(0);
  });

  test('absent when user is authenticated but lacks an admin group', async ({ page }) => {
    await mockMe(page, {
      authEnabled: true,
      email: 'alice@example.com',
      groups: ['platform-users'],
      canRunScan: false,
      features: {},
    });
    await page.goto(`${BASE}/scans`);
    await shellReady(page);
    await expect(page.getByText(/Manual scans are limited/)).toBeVisible();
    await expect(runScan(page)).toHaveCount(0);
  });

  test('visible when user is in an admin group', async ({ page }) => {
    await mockMe(page, ADMIN);
    await page.goto(BASE);
    await shellReady(page);

    const btn = runScan(page);
    await expect(btn).toBeVisible();
    await expect(btn).toBeEnabled();
    await expect(btn).toHaveText('Run scan');
  });

  test('clicking the button posts to /api/scan', async ({ page }) => {
    await mockMe(page, ADMIN);

    let scanPosts = 0;
    await page.route('**/api/scan', async (route) => {
      if (route.request().method() === 'POST') {
        scanPosts++;
        await route.fulfill({
          status: 202,
          contentType: 'application/json',
          body: JSON.stringify({ jobName: 'manual-scan-spec-stub', namespace: 'default' }),
        });
        return;
      }
      await route.continue();
    });

    await page.goto(BASE);
    await shellReady(page);
    await runScan(page).click();

    // While the poller waits for a newer report, the button reads "Scan
    // running…" and the job is shown. Seeing that confirms the POST went out.
    await expect(runScan(page)).toHaveText(/Scan running/, { timeout: 5_000 });
    await expect(runScan(page)).toBeDisabled();
    await expect(page.getByLabel('Scan job status')).toContainText('default/manual-scan-spec-stub');
    expect(scanPosts).toBe(1);
  });

  // Unmocked POST through the frontend's /api proxy to the real dashboard. Its
  // CSRF guard answers 403 unless the browser's Sec-Fetch-Site: same-origin
  // reaches it, so anything but 403 proves the proxy forwards the header. The
  // sandbox runs with OIDC off, where the dashboard answers 503 (scan endpoint
  // not configured) and starts no Job.
  test('the real POST /api/scan passes the dashboard CSRF guard', async ({ page }) => {
    await mockMe(page, ADMIN);
    await page.goto(BASE);
    await shellReady(page);
    const [resp] = await Promise.all([
      page.waitForResponse((r) => r.url().endsWith('/api/scan') && r.request().method() === 'POST'),
      runScan(page).click(),
    ]);
    expect(resp.status(), 'Sec-Fetch-Site must reach the dashboard').not.toBe(403);
  });

  // Stale-permission path: /api/me said the user could scan when the page
  // loaded, but /api/scan now returns 403. The UI must surface an error toast
  // and leave the button idle.
  test('403 on click surfaces an error toast and resets the button', async ({ page }) => {
    await mockMe(page, ADMIN);

    await page.route('**/api/scan', async (route) => {
      if (route.request().method() === 'POST') {
        await route.fulfill({ status: 403, contentType: 'text/plain', body: 'forbidden' });
        return;
      }
      await route.continue();
    });

    await page.goto(BASE);
    await shellReady(page);
    const btn = runScan(page);
    await btn.click();

    const toast = page.getByRole('region', { name: 'Notifications' });
    await expect(toast.getByText('Could not start scan')).toBeVisible({ timeout: 5_000 });
    await expect(toast.getByText(/Only members of the dashboard.s admin groups can run a scan/)).toBeVisible();

    await expect(btn).toBeEnabled();
    await expect(btn).toHaveText('Run scan');
    await expect(page.getByLabel('Scan job status')).toHaveCount(0);
  });
});
