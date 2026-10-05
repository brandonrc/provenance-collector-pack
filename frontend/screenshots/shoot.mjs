// Captures the dashboard pages in light and dark mode into docs/screenshots/.
// Works against any running instance in provenance mode: the VITE_API_MOCK=provenance preview
// (screenshots/run.sh) or Vite / nginx in front of a real dashboard (Integration Test workflow).
//
//   BASE_URL   where the SPA is served (default http://127.0.0.1:4173)
//   OUT_DIR    output directory (default /out)
//   THEMES     comma-separated subset of light,dark (default both)
//   DETAIL     image reference for the image-detail shot (default: the first row of /images)
import { chromium } from 'playwright';

const BASE = process.env.BASE_URL ?? 'http://127.0.0.1:4173';
const OUT = process.env.OUT_DIR ?? '/out';
const THEMES = (process.env.THEMES ?? 'light,dark').split(',').map((t) => t.trim()).filter(Boolean);

const PAGES = [
  { name: 'overview', path: '/', ready: 'main h1' },
  { name: 'images', path: '/images', ready: 'table[aria-label="Images"] tbody tr' },
  { name: 'image-detail', path: null, ready: 'main h1' },
  { name: 'supply-chain', path: '/supply-chain', ready: 'main h1' },
  { name: 'reports', path: '/reports', ready: 'table[aria-label="Reports"] tbody tr' },
];

async function settle(page, selector) {
  await page.waitForSelector(selector, { timeout: 30000 });
  await page.waitForFunction(() => !document.querySelector('[data-slot="skeleton"]'), null, { timeout: 30000 }).catch(() => {});
  await page.waitForTimeout(600);
}

const browser = await chromium.launch({ args: [`--unsafely-treat-insecure-origin-as-secure=${BASE}`] });
let detailPath = process.env.DETAIL ? `/images/${encodeURIComponent(process.env.DETAIL)}` : null;
for (const theme of THEMES) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1, colorScheme: theme, reducedMotion: 'reduce' });
  await context.addInitScript((mode) => localStorage.setItem('nebari:themeMode', mode), theme);
  const page = await context.newPage();
  page.on('pageerror', (e) => console.error(`[${theme}] pageerror`, e.message));
  for (const p of PAGES) {
    let path = p.path;
    if (p.name === 'image-detail') {
      if (!detailPath) {
        await page.goto(`${BASE}/images`, { waitUntil: 'networkidle' });
        await settle(page, 'table[aria-label="Images"] tbody tr a');
        detailPath = await page.locator('table[aria-label="Images"] tbody tr a').first().getAttribute('href');
      }
      path = detailPath;
    }
    await page.goto(`${BASE}${path}`, { waitUntil: 'networkidle' });
    await settle(page, p.ready);
    // the app scrolls inside <main>; grow the viewport so the whole page is captured
    const height = await page.evaluate(() => (document.querySelector('#main')?.scrollHeight ?? 900) + 64);
    await page.setViewportSize({ width: 1440, height: Math.min(Math.max(900, height), 2400) });
    await page.waitForTimeout(300);
    const file = `${OUT}/dashboard-${p.name}-${theme}.png`;
    await page.screenshot({ path: file });
    await page.setViewportSize({ width: 1440, height: 900 });
    console.log('saved', file);
  }
  await context.close();
}
await browser.close();
