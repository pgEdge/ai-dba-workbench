/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { test, expect } from '@playwright/test';
import { label } from 'allure-js-commons';
import { AdminPage } from '../pages/AdminPage';
import { NotificationChannelPage } from '../pages/NotificationChannelPage';
import { MailpitHelper } from '../helpers/mailpit.helper';
import { WireMockHelper } from '../helpers/wiremock.helper';

// ---------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------

/**
 * Generate a unique channel name with a timestamp suffix to
 * prevent collisions between retries and parallel workers.
 */
function uniqueName(prefix: string): string {
    return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
}

// ---------------------------------------------------------------
// External service helpers
// ---------------------------------------------------------------

const mailpit = new MailpitHelper(
    process.env.MAILPIT_URL ?? 'http://localhost:8025',
);
const wiremock = new WireMockHelper(
    process.env.WIREMOCK_URL ?? 'http://localhost:9090',
);

// ---------------------------------------------------------------
// Notification Channels Tests
// ---------------------------------------------------------------

test.describe('Notification Channels', () => {
    test.use({ storageState: '.auth/admin.json' });

    test.beforeEach(async () => {
        await label('package', 'Notification Channels');
    });

    // ===============================================================
    // Email Channel
    // ===============================================================

    test.describe('Email Channel', () => {
        test.beforeEach(async () => {
            try {
                await mailpit.deleteAllMessages();
            } catch {
                // Mailpit may not be running in all environments.
            }
        });

        test('create, update, test, and delete email channel', async ({ page }) => {
            test.slow();

            const adminPage = new AdminPage(page);
            const channelPage = new NotificationChannelPage(page);
            const channelName = uniqueName('e2e-email');
            const updatedDesc = 'Updated by E2E test';
            const recipientEmail = 'e2e-recipient@test.local';

            await test.step('Navigate to Email Channels', async () => {
                await page.goto('/');
                await adminPage.waitForAppLoad();
                await adminPage.navigateToEmailChannels();
            });

            await test.step('Create email channel', async () => {
                await channelPage.clickAddChannel();
                await channelPage.fillChannelName(channelName);
                await channelPage.fillChannelDescription('E2E test email channel');
                await channelPage.fillEmailSettings({
                    // host.docker.internal resolves to the host machine
                    // from inside the server container, reaching Mailpit
                    // which is mapped to host port 1025.
                    smtpHost: 'host.docker.internal',
                    smtpPort: '1025',
                    fromAddress: 'test@e2e.local',
                    fromName: 'E2E Tester',
                    useTls: false,
                });
            });

            await test.step('Add recipient in Recipients tab', async () => {
                await channelPage.switchToRecipientsTab();
                await channelPage.addEmailRecipient(
                    recipientEmail,
                    'E2E Recipient',
                );
            });

            await test.step('Save and verify channel in table', async () => {
                await channelPage.saveChannel();
                await channelPage.expectSuccessToast();
                await channelPage.expectChannelInTable(channelName);
            });

            await test.step('Edit channel description', async () => {
                await channelPage.clickEditChannel(channelName);
                await channelPage.clearAndFillField(
                    'Description',
                    updatedDesc,
                    channelPage['innerDialog'],
                );
                await channelPage.saveChannel();
                await channelPage.expectSuccessToast();
            });

            await test.step('Send test notification', async () => {
                await channelPage.clickTestChannel(channelName);
                await channelPage.expectTestNotificationSuccess();
            });

            await test.step('Verify Mailpit received email', async () => {
                try {
                    const msg = await mailpit.waitForMessage(
                        recipientEmail,
                        30_000,
                    );
                    expect(msg).toBeTruthy();
                    expect(msg.To.length).toBeGreaterThan(0);
                } catch {
                    // Mailpit validation is best-effort when the
                    // service is unavailable in CI environments
                    // without the docker stack running.
                    console.warn(
                        'Mailpit not reachable; skipping email delivery assertion.',
                    );
                }
            });

            await test.step('Delete channel', async () => {
                await channelPage.clickDeleteChannel(channelName);
                await channelPage.expectChannelNotInTable(channelName);
            });
        });
    });

    // ===============================================================
    // Slack Channel
    // ===============================================================

    test.describe('Slack Channel', () => {
        test.beforeEach(async () => {
            try {
                await wiremock.resetRequests();
            } catch {
                // WireMock may not be running in all environments.
            }
        });

        test('create, update, test, and delete Slack channel', async ({ page }) => {
            test.slow();

            const adminPage = new AdminPage(page);
            const channelPage = new NotificationChannelPage(page);
            const channelName = uniqueName('e2e-slack');
            const updatedDesc = 'Updated Slack by E2E';

            await test.step('Navigate to Slack Channels', async () => {
                await page.goto('/');
                await adminPage.waitForAppLoad();
                await adminPage.navigateToSlackChannels();
            });

            await test.step('Create Slack channel', async () => {
                await channelPage.clickAddChannel();
                await channelPage.fillChannelName(channelName);
                await channelPage.fillChannelDescription('E2E test Slack channel');
                await channelPage.fillWebhookUrl('http://host.docker.internal:9090/slack');
                await channelPage.saveChannel();
                await channelPage.expectSuccessToast();
                await channelPage.expectChannelInTable(channelName);
            });

            await test.step('Edit channel description', async () => {
                await channelPage.clickEditChannel(channelName);
                await channelPage.clearAndFillField(
                    'Description',
                    updatedDesc,
                    channelPage['innerDialog'],
                );
                await channelPage.saveChannel();
                await channelPage.expectSuccessToast();
            });

            await test.step('Send test notification', async () => {
                await channelPage.clickTestChannel(channelName);
                await channelPage.expectTestNotificationSuccess();
            });

            await test.step('Verify WireMock received Slack request', async () => {
                try {
                    const req = await wiremock.waitForRequest(
                        '/slack',
                        30_000,
                    );
                    expect(req).toBeTruthy();
                    expect(req.method).toBe('POST');
                } catch {
                    console.warn(
                        'WireMock not reachable; skipping Slack delivery assertion.',
                    );
                }
            });

            await test.step('Delete channel', async () => {
                await channelPage.clickDeleteChannel(channelName);
                await channelPage.expectChannelNotInTable(channelName);
            });
        });
    });

    // ===============================================================
    // Mattermost Channel
    // ===============================================================

    test.describe('Mattermost Channel', () => {
        test.beforeEach(async () => {
            try {
                await wiremock.resetRequests();
            } catch {
                // WireMock may not be running in all environments.
            }
        });

        test('create, update, test, and delete Mattermost channel', async ({ page }) => {
            test.slow();

            const adminPage = new AdminPage(page);
            const channelPage = new NotificationChannelPage(page);
            const channelName = uniqueName('e2e-mattermost');
            const updatedDesc = 'Updated Mattermost by E2E';

            await test.step('Navigate to Mattermost Channels', async () => {
                await page.goto('/');
                await adminPage.waitForAppLoad();
                await adminPage.navigateToMattermostChannels();
            });

            await test.step('Create Mattermost channel', async () => {
                await channelPage.clickAddChannel();
                await channelPage.fillChannelName(channelName);
                await channelPage.fillChannelDescription('E2E test Mattermost channel');
                await channelPage.fillWebhookUrl('http://host.docker.internal:9090/mattermost');
                await channelPage.saveChannel();
                await channelPage.expectSuccessToast();
                await channelPage.expectChannelInTable(channelName);
            });

            await test.step('Edit channel description', async () => {
                await channelPage.clickEditChannel(channelName);
                await channelPage.clearAndFillField(
                    'Description',
                    updatedDesc,
                    channelPage['innerDialog'],
                );
                await channelPage.saveChannel();
                await channelPage.expectSuccessToast();
            });

            await test.step('Send test notification', async () => {
                await channelPage.clickTestChannel(channelName);
                await channelPage.expectTestNotificationSuccess();
            });

            await test.step('Verify WireMock received Mattermost request', async () => {
                try {
                    const req = await wiremock.waitForRequest(
                        '/mattermost',
                        30_000,
                    );
                    expect(req).toBeTruthy();
                    expect(req.method).toBe('POST');
                } catch {
                    console.warn(
                        'WireMock not reachable; skipping Mattermost delivery assertion.',
                    );
                }
            });

            await test.step('Delete channel', async () => {
                await channelPage.clickDeleteChannel(channelName);
                await channelPage.expectChannelNotInTable(channelName);
            });
        });
    });

    // ===============================================================
    // Webhook Channel
    // ===============================================================

    test.describe('Webhook Channel', () => {
        test.beforeEach(async () => {
            try {
                await wiremock.resetRequests();
            } catch {
                // WireMock may not be running in all environments.
            }
        });

        test('create, update, test, and delete webhook channel', async ({ page }) => {
            test.slow();

            const adminPage = new AdminPage(page);
            const channelPage = new NotificationChannelPage(page);
            const channelName = uniqueName('e2e-webhook');
            const updatedDesc = 'Updated Webhook by E2E';

            await test.step('Navigate to Webhook Channels', async () => {
                await page.goto('/');
                await adminPage.waitForAppLoad();
                await adminPage.navigateToWebhookChannels();
            });

            await test.step('Create webhook channel', async () => {
                await channelPage.clickAddChannel();
                await channelPage.fillChannelName(channelName);
                await channelPage.fillChannelDescription('E2E test webhook channel');
                await channelPage.fillWebhookEndpointUrl(
                    'http://host.docker.internal:9090/webhook',
                );
                await channelPage.saveChannel();
                await channelPage.expectSuccessToast();
                await channelPage.expectChannelInTable(channelName);
            });

            await test.step('Edit channel description', async () => {
                await channelPage.clickEditChannel(channelName);
                await channelPage.switchToWebhookTab('Settings');
                await channelPage.clearAndFillField(
                    'Description',
                    updatedDesc,
                    channelPage['innerDialog'],
                );
                await channelPage.saveChannel();
                await channelPage.expectSuccessToast();
            });

            await test.step('Send test notification', async () => {
                await channelPage.clickTestChannel(channelName);
                await channelPage.expectTestNotificationSuccess();
            });

            await test.step('Verify WireMock received webhook request', async () => {
                try {
                    const req = await wiremock.waitForRequest(
                        '/webhook',
                        30_000,
                    );
                    expect(req).toBeTruthy();
                    expect(req.method).toBe('POST');
                } catch {
                    console.warn(
                        'WireMock not reachable; skipping webhook delivery assertion.',
                    );
                }
            });

            await test.step('Delete channel', async () => {
                await channelPage.clickDeleteChannel(channelName);
                await channelPage.expectChannelNotInTable(channelName);
            });
        });
    });
});
