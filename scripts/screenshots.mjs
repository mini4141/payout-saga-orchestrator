// Headless screenshots of the ops UI (and Temporal UI), driven by Playwright.
// Usage: node scripts/screenshots.mjs <outDir> <opsUiUrl> <temporalUiUrl> <retryPayoutId> <compensatedPayoutId> [prefix]
// Called by scripts/capture-proof.sh; Playwright lives in .local/pw (gitignored).
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(here, "..", ".local", "pw", "package.json"));
const { chromium } = require("playwright");

const [outDir, ops, temporalUi, retryId, compId, mode = "local"] = process.argv.slice(2);
const shot = async (page, name, opts = {}) => {
  await page.screenshot({ path: path.join(outDir, name), fullPage: true, ...opts });
  console.log("saved", name);
};

const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 }, deviceScaleFactor: 1 });
const page = await ctx.newPage();
const go = async (url) => {
  await page.goto(url, { waitUntil: "networkidle" });
  await page.waitForTimeout(400);
};

if (mode === "cloud") {
  await go(`${ops}/`);
  await shot(page, "01-cloud-ops-ui.png");
  await go(`${ops}/payouts/${retryId}`);
  await shot(page, "02-cloud-timeline.png");
  await go(`${ops}/invariants`);
  await shot(page, "03-cloud-invariants.png");
} else {
  await go(`${ops}/`);
  await shot(page, "01-payouts.png");
  await go(`${ops}/payouts/${retryId}`);
  await shot(page, "02-payout-timeline-retries.png");
  await go(`${ops}/payouts/${compId}`);
  await shot(page, "03-compensated.png");
  await go(`${ops}/inject`);
  await shot(page, "04-inject.png");
  await go(`${ops}/recon`);
  await shot(page, "05-recon.png");
  await go(`${ops}/invariants`);
  await shot(page, "06-invariants.png");
  await page.goto(`${temporalUi}/namespaces/default/workflows`, { waitUntil: "networkidle" });
  await page.getByText("payout-").first().waitFor({ timeout: 30000 }).catch(() => {});
  await page.waitForTimeout(1500);
  await shot(page, "07-temporal-ui.png");
}
await browser.close();
