// Report history switch end-to-end test.
//
// Regression coverage for https://github.com/nebari-dev/provenance-collector-pack/issues/19:
// viewing a non-latest report must swap the Images table and the Overview
// tiles, not just one of them.
//
// Preconditions (set up by the calling workflow):
//   - The frontend is reachable at $DASHBOARD_URL, proxying /api to the
//     dashboard.
//   - Exactly two provenance reports exist on disk. The newer one must include
//     an image whose name contains $TIMELINE_NEW_IMAGE (default
//     "traefik/whoami") that is absent from the older one.

const { test, expect } = require('@playwright/test');
const { BASE, shellReady, imagesSettled, imagesTotal, gotoSection } = require('./helpers');

const NEW_IMAGE_MARKER = process.env.TIMELINE_NEW_IMAGE || 'traefik/whoami';

// overviewUniqueImages reads "x of N images" from the Overview's Signed tile; N is the
// report's unique-image count.
async function overviewUniqueImages(page) {
  const tile = page.locator('[data-slot="card"]').filter({ has: page.getByText('Signed', { exact: true }) });
  await expect(tile).toContainText(/of \d+ images?/);
  const m = ((await tile.textContent()) || '').match(/of (\d+) images?/);
  return parseInt(m[1], 10);
}

// markerCount filters the Images table by the marker and returns the match count,
// then clears the filter again. Each step waits for the page to render the new
// filter (the "Clear filters" button follows the URL state), not just for the
// URL, so the count is never read off the previous query's rows.
async function markerCount(page) {
  const search = page.getByRole('searchbox', { name: 'Search images' }).or(page.getByLabel('Search images'));
  const clearFilters = page.getByRole('button', { name: 'Clear filters' });
  await search.first().fill(NEW_IMAGE_MARKER);
  await expect(page).toHaveURL(/[?&]q=/);
  await expect(clearFilters).toBeVisible();
  const n = await imagesTotal(page);
  await search.first().fill('');
  await expect(page).not.toHaveURL(/[?&]q=/);
  await expect(clearFilters).toHaveCount(0);
  await imagesSettled(page);
  return n;
}

test('viewing an older report updates the Overview tiles AND the Images table', async ({ page }) => {
  await page.goto(`${BASE}/reports`);
  await shellReady(page);

  const rows = page.getByRole('table', { name: 'Reports' }).locator('tbody tr');
  await expect(rows, 'expected exactly two reports in the history').toHaveCount(2, { timeout: 10_000 });

  // Reports are sorted DESC by generatedAt; row 0 is the newest and is the
  // active dataset by default.
  await expect(rows.nth(0)).toContainText('Latest · viewing');
  const olderFile = ((await rows.nth(1).locator('.font-mono').first().textContent()) || '').trim();
  expect(olderFile).toMatch(/\.json$/);

  // --- Newest report (default state) ----------------------------------------
  await gotoSection(page, 'Overview');
  const newestStat = await overviewUniqueImages(page);
  await gotoSection(page, 'Images');
  const newestTotal = await imagesTotal(page);
  const newestHasNewImage = await markerCount(page);

  expect(newestTotal, 'newest report should list images').toBeGreaterThan(0);
  expect(newestHasNewImage, `newest report must contain "${NEW_IMAGE_MARKER}"`).toBeGreaterThan(0);

  // --- Older report ---------------------------------------------------------
  // Switching re-fetches the older report and invalidates every dataset query;
  // pages keep showing the newest data until their refetch lands. Wait for the
  // report itself, then poll the views until they show it.
  await gotoSection(page, 'Reports');
  const olderReport = page.waitForResponse(
    (r) => r.ok() && decodeURIComponent(new URL(r.url()).pathname).endsWith(`/api/reports/${olderFile}`),
  );
  await page.getByRole('button', { name: `View report ${olderFile}` }).click();
  await expect(page.getByText('Viewing an earlier report')).toBeVisible();

  await gotoSection(page, 'Overview');
  await olderReport;
  await expect.poll(() => overviewUniqueImages(page), { message: 'Overview tiles should switch to the older report' }).toBeLessThan(newestStat);
  const olderStat = await overviewUniqueImages(page);
  await gotoSection(page, 'Images');
  await expect(page.getByText('Viewing an earlier report')).toBeVisible();
  await expect.poll(() => imagesTotal(page), { message: 'Images table should switch to the older report' }).toBeLessThan(newestTotal);
  const olderTotal = await imagesTotal(page);
  const olderHasNewImage = await markerCount(page);

  // 1. The Overview tiles moved.
  expect(olderStat, 'older report should have fewer unique images than newest').toBeLessThan(newestStat);
  // 2. The Images table moved with them (#19: tiles updated, table did not).
  expect(olderTotal, 'older report should list fewer images than newest').toBeLessThan(newestTotal);
  // 3. The image added between the two scans is not in the older table.
  expect(olderHasNewImage, `older report must NOT contain "${NEW_IMAGE_MARKER}"`).toBe(0);

  // --- Back to latest -------------------------------------------------------
  await page.getByRole('button', { name: 'Back to latest' }).click();
  await expect(page.getByText('Viewing an earlier report')).toHaveCount(0);
  await expect.poll(() => imagesTotal(page), { message: 'Images table should switch back to the newest report' }).toBe(newestTotal);
});
