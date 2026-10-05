// Shared helpers for the dashboard e2e specs.
//
// DASHBOARD_URL points at the frontend (Vite dev server or the nginx image),
// which proxies /api/* to the dashboard. The sandbox installs the dashboard with
// OIDC off and the dev server serves a config.json without a keycloak block, so
// the SPA sends no bearer and needs no login.

const { expect } = require('@playwright/test');

const BASE = process.env.DASHBOARD_URL || 'http://localhost:5173';

// shellReady waits for the app shell (sidebar) and fails on a crashed route.
async function shellReady(page) {
  await expect(page.getByRole('navigation', { name: /sections$/ })).toBeVisible();
  await expect(page.getByText(/Unexpected Application Error/i)).toHaveCount(0);
  await expect(page.getByText('This page failed to load')).toHaveCount(0);
}

// mockMe answers /api/me with the given payload. Call before page.goto().
async function mockMe(page, payload) {
  await page.route('**/api/me', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(payload) }),
  );
}

// imagesTotal reads the Images table's pager ("1–5 of 5") on the current page;
// 0 when the table shows its empty state.
async function imagesTotal(page) {
  const table = page.getByRole('table', { name: 'Images' });
  await expect(table).toBeVisible();
  await expect(table).not.toHaveAttribute('aria-busy', 'true');
  const pager = page.getByText(/^\d+–\d+ of [\d,]+$/);
  const empty = page.getByText(/^No images (match these filters|yet)$/);
  await expect(pager.or(empty)).toBeVisible();
  if (await empty.isVisible()) return 0;
  const m = ((await pager.textContent()) || '').match(/of ([\d,]+)$/);
  return m ? parseInt(m[1].replace(/,/g, ''), 10) : 0;
}

// gotoSection navigates through the sidebar (no reload, so the selected
// report dataset is kept).
async function gotoSection(page, name) {
  await page.getByRole('navigation', { name: /sections$/ }).getByRole('link', { name, exact: true }).click();
  await shellReady(page);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(name);
}

module.exports = { BASE, shellReady, mockMe, imagesTotal, gotoSection };
