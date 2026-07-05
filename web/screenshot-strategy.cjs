const { chromium } = require('playwright');

(async () => {
  const browser = await chromium.launch();
  const context = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const page = await context.newPage();
  await page.addInitScript(() => {
    window.__E2E_AUTH__ = true;
    localStorage.setItem('xt-token', 'dev-token');
  });

  await page.goto('http://localhost:5174/strategy');
  await page.waitForTimeout(2000);
  await page.locator('button', { hasText: '策略管理' }).first().click();
  await page.waitForTimeout(2000);
  await page.locator('button[aria-label="创建策略"]').click();
  await page.waitForTimeout(1500);

  // Switch to spot market inside modal
  await page.locator('[role="dialog"] button', { hasText: /^现货$/ }).click();
  await page.waitForTimeout(1000);

  // Go to CRA params
  await page.locator('[role="dialog"] button', { hasText: '下一步' }).first().click();
  await page.waitForTimeout(1000);
  await page.screenshot({ path: '/tmp/strategy-create-spot.png' });

  await browser.close();
})();
