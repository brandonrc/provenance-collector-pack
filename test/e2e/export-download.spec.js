// Report download validation test.
//
// Drives the Reports page's JSON / CSV / Markdown downloads for the latest
// report, captures the actual download event, and shape-validates the file
// against the dashboard's own JSON API (the source of truth — the export
// endpoints render from the same on-disk report).
//
// Preconditions (set up by the calling workflow):
//   - The frontend is reachable at $DASHBOARD_URL, proxying /api to the
//     dashboard.
//   - At least one provenance report exists on disk.

const { test, expect } = require('@playwright/test');
const fs = require('fs');
const { BASE, shellReady } = require('./helpers');

test.describe('Report downloads', () => {
  let truth;

  test.beforeAll(async ({ request }) => {
    // Source of truth: the API response for the latest report, through the
    // frontend's /api proxy.
    const list = await request.get(`${BASE}/api/reports`);
    expect(list.ok(), 'GET /api/reports must succeed').toBeTruthy();
    const reports = await list.json();
    expect(reports.length, 'expected at least one report').toBeGreaterThan(0);
    const latest = await request.get(`${BASE}/api/reports/${reports[0].filename}`);
    expect(latest.ok(), 'GET /api/reports/<latest> must succeed').toBeTruthy();
    truth = await latest.json();
  });

  test('CSV download has the expected header and one row per image', async ({ page }) => {
    const csv = await download(page, 'Export latest report as CSV');

    const lines = csv.split('\n').filter((l) => l.length > 0);
    const header = lines[0];
    const dataRows = lines.slice(1);

    expect(header, 'CSV header must lead with the expected schema columns').toBe(
      'Image,Namespace,Workload Kind,Workload Name,Digest,Signed,Verified,SLSA Provenance,SBOM,SBOM Format,Update Available,Current Tag,Latest In Major',
    );

    expect(dataRows.length, 'one CSV row per image in the source report').toBe(truth.images.length);

    for (const img of truth.images) {
      const found = dataRows.some((row) => row.startsWith(csvField(img.image) + ','));
      expect(found, `CSV must contain a row for ${img.image}`).toBeTruthy();
    }
  });

  test('Markdown download has the expected sections', async ({ page }) => {
    const md = await download(page, 'Export latest report as Markdown');

    expect(md).toContain('# Provenance Report');
    expect(md).toContain('## Summary');
    expect(md).toContain('## Container Images');

    const imgSection = md.split('## Container Images')[1];
    expect(imgSection, 'Container Images section must exist').toBeTruthy();
    const imgTable = imgSection.split('## ')[0];
    const imgDataRows = markdownDataRows(imgTable, '| Image |');
    expect(imgDataRows.length, 'one Markdown image row per image in the source report').toBe(
      truth.images.length,
    );

    if (truth.helmReleases && truth.helmReleases.length > 0) {
      expect(md).toContain('## Helm Releases');
      const helmSection = md.split('## Helm Releases')[1];
      const helmRows = markdownDataRows(helmSection, '| Release |');
      expect(helmRows.length, 'one Markdown helm row per release in the source report').toBe(
        truth.helmReleases.length,
      );
    }
  });

  test('JSON download matches the API response byte-shape', async ({ page }) => {
    const raw = await download(page, 'Download latest report as JSON');
    const parsed = JSON.parse(raw);

    expect(parsed.metadata.generatedAt).toBe(truth.metadata.generatedAt);
    expect(parsed.summary.uniqueImages).toBe(truth.summary.uniqueImages);
    expect(parsed.summary.totalImages).toBe(truth.summary.totalImages);
    expect(parsed.images.length).toBe(truth.images.length);
    expect((parsed.helmReleases || []).length).toBe((truth.helmReleases || []).length);
  });

  test('an earlier report has its own downloads', async ({ page, request }) => {
    const reports = await (await request.get(`${BASE}/api/reports`)).json();
    const target = reports[reports.length - 1];
    const report = await (await request.get(`${BASE}/api/reports/${target.filename}`)).json();
    const csv = await download(page, `Export ${target.filename} as CSV`);
    const rows = csv.split('\n').filter((l) => l.length > 0).slice(1);
    expect(rows.length, `one CSV row per image in ${target.filename}`).toBe(report.images.length);
  });
});

// download clicks a download button on the Reports page, captures the download
// event, and returns the file contents.
async function download(page, buttonName) {
  await page.goto(`${BASE}/reports`);
  await shellReady(page);
  const btn = page.getByRole('button', { name: buttonName });
  await expect(btn).toBeVisible();
  const [dl] = await Promise.all([page.waitForEvent('download'), btn.click()]);
  return fs.readFileSync(await dl.path(), 'utf-8');
}

// csvField mirrors the server's csvEscape so we can compare predictably. Only
// covers the inputs we actually test against (image names).
function csvField(s) {
  if (/[,"\n]/.test(s)) {
    return '"' + s.replace(/"/g, '""') + '"';
  }
  return s;
}

// markdownDataRows returns the data rows of a Markdown table from a section
// blob, skipping the header (matched by headerNeedle) and the separator row.
function markdownDataRows(section, headerNeedle) {
  return section
    .split('\n')
    .filter((l) => l.startsWith('|'))
    .filter((l) => !l.includes(headerNeedle))
    .filter((l) => !/^\|[-:|\s]+$/.test(l));
}
