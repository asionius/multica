import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";

// Reports are immutable and have no delete API, so every test run publishes
// under a unique version key and cleans the rows up through the DB afterwards.
function uniqueVersion(): string {
  return `e2e-${Date.now()}`;
}

function reportPayload(version: string, publishKey: string, marker: string) {
  return {
    schema_version: 1,
    version,
    comparison_version: "e2e-baseline",
    title: `E2E 运营报告 ${version}`,
    metric_version: "historical-comment-inference/v1",
    observation_as_of: "2026-10-28T02:11:14Z",
    body_markdown: `# E2E 运营报告\n\n${marker}\n\n| 指标 | 上版 | 本版 |\n|---|---|---|\n| **QA 逃逸** | 30 | 暂无 |\n`,
    publish_key: publishKey,
  };
}

test.describe("Dev reports", () => {
  test("renders the published body and can switch revisions", async ({ page }) => {
    const api = await createTestApi();
    const version = uniqueVersion();
    const firstMarker = `E2E-MARKER-${version}-r1`;
    const secondMarker = `E2E-MARKER-${version}-r2`;

    try {
      const first = await api.publishDevReport(reportPayload(version, `${version}-r1`, firstMarker));
      expect(first.status).toBe(201);
      const second = await api.publishDevReport(reportPayload(version, `${version}-r2`, secondMarker));
      expect(second.status).toBe(201);

      const slug = await loginAsDefault(page);
      await page.goto(`/${slug}/dev-report`);
      await expect(page.getByText(secondMarker)).toBeVisible();

      // Revision picker appears once a version has more than one revision.
      await page.getByRole("combobox").last().click();
      await page.getByRole("option", { name: /r1/ }).click();
      await expect(page.getByText(firstMarker)).toBeVisible();
    } finally {
      await api.deleteDevReportsByVersion(version);
    }
  });

  test("a revision deep link keeps its content after a refresh", async ({ page }) => {
    const api = await createTestApi();
    const version = uniqueVersion();
    const firstMarker = `E2E-MARKER-${version}-r1`;
    const secondMarker = `E2E-MARKER-${version}-r2`;

    try {
      const first = await api.publishDevReport(reportPayload(version, `${version}-r1`, firstMarker));
      expect(first.status).toBe(201);
      const reportId = (first.body as { id: string }).id;
      const second = await api.publishDevReport(reportPayload(version, `${version}-r2`, secondMarker));
      expect(second.status).toBe(201);

      const slug = await loginAsDefault(page);
      await page.goto(`/${slug}/dev-report/${reportId}`);
      await expect(page.getByText(firstMarker)).toBeVisible();

      await page.reload();
      // The old revision is immutable: refreshing must not jump to r2.
      await expect(page.getByText(firstMarker)).toBeVisible();
      await expect(page.getByText(secondMarker)).not.toBeVisible();
    } finally {
      await api.deleteDevReportsByVersion(version);
    }
  });

  test("unknown report ids show an explicit unreadable state", async ({ page }) => {
    const slug = await loginAsDefault(page);
    await page.goto(`/${slug}/dev-report/00000000-0000-0000-0000-000000000001`);
    await expect(page.getByText(/Report could not be loaded|报告无法读取/)).toBeVisible();
  });

  test("public deep links are readable without a session", async ({ page }) => {
    const api = await createTestApi();
    const version = uniqueVersion();
    const marker = `E2E-PUBLIC-${version}`;
    try {
      const created = await api.publishDevReport(reportPayload(version, `${version}-r1`, marker));
      expect(created.status).toBe(201);
      const reportId = (created.body as { id: string }).id;

      const workspaces = await api.getWorkspaces();
      const slug = workspaces.find((w) => w.slug === "e2e-workspace")?.slug ?? workspaces[0].slug;

      // Drop every credential, then load the report like an external visitor.
      await page.goto("/login");
      await page.evaluate(() => localStorage.clear());
      await page.context().clearCookies();

      await page.goto(`/${slug}/dev-report/${reportId}`);
      await expect(page.getByText(marker)).toBeVisible();
      expect(page.url()).not.toContain("/login");
    } finally {
      await api.deleteDevReportsByVersion(version);
    }
  });
});
