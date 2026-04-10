/*
 * pgEdge AI DBA Workbench — Guided Tour
 *
 * Self-initializing tour definition using Driver.js (IIFE bundle).
 * Loaded by loader.js after the Driver.js library is available.
 *
 * Architecture:
 *   24 steps across 6 parts, plus helper functions for minimize,
 *   resume, skip-to-end, API key modal, and Make It Yours overlay.
 */
(function () {
    "use strict";

    var HELPER_BASE = "/walkthrough/api";

    // -----------------------------------------------------------------------
    // State
    // -----------------------------------------------------------------------

    var apiKeyConfigured = false;
    var currentStep = 0;
    var isMinimized = false;
    var driverInstance = null;
    var resumePill = null;

    // -----------------------------------------------------------------------
    // Utility helpers
    // -----------------------------------------------------------------------

    /** Wait for a selector to appear in the DOM. */
    function waitForElement(selector, timeout) {
        timeout = timeout || 5000;
        return new Promise(function (resolve, reject) {
            var el = document.querySelector(selector);
            if (el) { return resolve(el); }
            var elapsed = 0;
            var interval = setInterval(function () {
                el = document.querySelector(selector);
                elapsed += 100;
                if (el) {
                    clearInterval(interval);
                    resolve(el);
                } else if (elapsed >= timeout) {
                    clearInterval(interval);
                    reject(new Error("Timeout waiting for " + selector));
                }
            }, 100);
        });
    }

    /** Detect whether the page is using dark mode. */
    function isDark() {
        return document.documentElement.classList.contains("dark") ||
            document.querySelector(".MuiCssBaseline-root")?.closest("[data-mui-color-scheme='dark']") !== null ||
            getComputedStyle(document.body).backgroundColor.match(/rgb\((\d+)/) ?
                parseInt(getComputedStyle(document.body).backgroundColor.match(/rgb\((\d+)/)[1]) < 50 : false;
    }

    /** Build an AI-dependent description, with fallback if no API key. */
    function aiDesc(normalText, fallbackImage) {
        if (apiKeyConfigured) {
            return normalText;
        }
        var html = normalText +
            '<br><br><em>An API key is required for live AI features.</em>';
        if (fallbackImage) {
            html += '<br><img class="wt-fallback-img" src="/walkthrough/images/' +
                fallbackImage + '" alt="Example screenshot">';
        }
        html += '<br><button class="wt-api-key-btn" onclick="window.__wtShowApiKeyModal()">Add API Key</button>';
        return html;
    }

    // -----------------------------------------------------------------------
    // Check API key status from the walkthrough helper
    // -----------------------------------------------------------------------

    function checkStatus() {
        return fetch(HELPER_BASE + "/status")
            .then(function (r) { return r.json(); })
            .then(function (data) {
                apiKeyConfigured = !!data.api_key_configured;
            })
            .catch(function () {
                apiKeyConfigured = false;
            });
    }

    // -----------------------------------------------------------------------
    // API Key modal
    // -----------------------------------------------------------------------

    function showApiKeyModal() {
        var existing = document.querySelector(".wt-apikey-modal");
        if (existing) { existing.remove(); }

        var overlay = document.createElement("div");
        overlay.className = "wt-apikey-modal";
        overlay.innerHTML =
            '<div class="wt-apikey-card">' +
            '  <h3>Add an AI Provider API Key</h3>' +
            '  <p>Enter your Anthropic API key to enable AI features ' +
            '  such as AI Overview, alert analysis, and Ask Ellie.</p>' +
            '  <form id="wt-apikey-form">' +
            '    <input type="password" id="wt-apikey-input" ' +
            '      placeholder="sk-ant-..." autocomplete="off">' +
            '    <button type="submit">Save Key</button>' +
            '    <button type="button" id="wt-apikey-cancel">Cancel</button>' +
            '  </form>' +
            '</div>';
        document.body.appendChild(overlay);

        overlay.querySelector("#wt-apikey-cancel").addEventListener("click", function () {
            overlay.remove();
        });

        overlay.querySelector("#wt-apikey-form").addEventListener("submit", function (e) {
            e.preventDefault();
            var key = overlay.querySelector("#wt-apikey-input").value.trim();
            if (!key) { return; }
            fetch(HELPER_BASE + "/set-api-key", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ api_key: key }),
            })
                .then(function (r) { return r.json(); })
                .then(function (data) {
                    if (data.success) {
                        apiKeyConfigured = true;
                        overlay.remove();
                    }
                })
                .catch(function () {
                    alert("Failed to save API key. Please try again.");
                });
        });
    }

    // Expose for inline onclick in popover HTML
    window.__wtShowApiKeyModal = showApiKeyModal;

    // -----------------------------------------------------------------------
    // Make It Yours overlay
    // -----------------------------------------------------------------------

    function showMakeYoursOverlay() {
        var existing = document.querySelector(".wt-make-yours-overlay");
        if (existing) { existing.remove(); }

        var overlay = document.createElement("div");
        overlay.className = "wt-make-yours-overlay";
        overlay.innerHTML =
            '<div class="wt-make-yours-card">' +
            '  <h2>Make It Yours</h2>' +
            '  <p>You have seen what the AI DBA Workbench can do with ' +
            '  the demo data. Ready to try it with your own database?</p>' +
            '  <button class="wt-choice-btn" id="wt-choice-connect">' +
            '    <strong>Connect my database</strong>' +
            '    Replace the demo with your own PostgreSQL server.' +
            '  </button>' +
            '  <button class="wt-choice-btn" id="wt-choice-apikey">' +
            '    <strong>Add my API key</strong>' +
            '    Enable AI features with your Anthropic key.' +
            '  </button>' +
            '  <button class="wt-choice-btn" id="wt-choice-explore">' +
            '    <strong>Keep exploring</strong>' +
            '    Continue using the demo data on your own.' +
            '  </button>' +
            '  <button class="wt-choice-btn" id="wt-choice-dismiss">' +
            '    <strong>Dismiss the tour</strong>' +
            '    Close and do not show the tour again.' +
            '  </button>' +
            '  <div id="wt-connect-form-area"></div>' +
            '</div>';
        document.body.appendChild(overlay);

        // Keep exploring
        overlay.querySelector("#wt-choice-explore").addEventListener("click", function () {
            sessionStorage.removeItem("wt-current-step");
            overlay.remove();
        });

        // Dismiss permanently
        overlay.querySelector("#wt-choice-dismiss").addEventListener("click", function () {
            localStorage.setItem("wt-tour-closed", "true");
            sessionStorage.removeItem("wt-current-step");
            overlay.remove();
        });

        // Add API key
        overlay.querySelector("#wt-choice-apikey").addEventListener("click", function () {
            overlay.remove();
            showApiKeyModal();
        });

        // Connect database
        overlay.querySelector("#wt-choice-connect").addEventListener("click", function () {
            var area = overlay.querySelector("#wt-connect-form-area");
            if (area.querySelector(".wt-connect-form")) { return; }
            area.innerHTML =
                '<form class="wt-connect-form" id="wt-connect-real-form">' +
                '  <label>Connection name' +
                '    <input type="text" name="name" value="my-database"></label>' +
                '  <label>Host' +
                '    <input type="text" name="host" placeholder="db.example.com" required></label>' +
                '  <label>Port' +
                '    <input type="number" name="port" value="5432"></label>' +
                '  <label>Database' +
                '    <input type="text" name="database_name" placeholder="mydb" required></label>' +
                '  <label>Username' +
                '    <input type="text" name="username" placeholder="postgres" required></label>' +
                '  <label>Password' +
                '    <input type="password" name="password" required></label>' +
                '  <label>SSL mode' +
                '    <select name="ssl_mode">' +
                '      <option value="prefer">prefer</option>' +
                '      <option value="require">require</option>' +
                '      <option value="disable">disable</option>' +
                '    </select></label>' +
                '  <label>Anthropic API key (optional)' +
                '    <input type="password" name="api_key" placeholder="sk-ant-..."></label>' +
                '  <button type="submit">Connect</button>' +
                '</form>';

            area.querySelector("#wt-connect-real-form").addEventListener("submit", function (e) {
                e.preventDefault();
                var form = e.target;
                var payload = {
                    name: form.name.value,
                    host: form.host.value,
                    port: parseInt(form.port.value, 10) || 5432,
                    database_name: form.database_name.value,
                    username: form.username.value,
                    password: form.password.value,
                    ssl_mode: form.ssl_mode.value,
                    api_key: form.api_key.value,
                };
                fetch(HELPER_BASE + "/add-connection", {
                    method: "POST",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify(payload),
                })
                    .then(function (r) { return r.json(); })
                    .then(function (data) {
                        if (data.success) {
                            localStorage.setItem("wt-tour-closed", "true");
                            sessionStorage.removeItem("wt-current-step");
                            overlay.remove();
                            // Reload to pick up the new connection
                            window.location.reload();
                        } else {
                            alert("Connection failed: " + (data.error || "unknown error"));
                        }
                    })
                    .catch(function () {
                        alert("Request failed. Check the connection details and try again.");
                    });
            });
        });
    }

    // -----------------------------------------------------------------------
    // Resume pill
    // -----------------------------------------------------------------------

    function createResumePill() {
        if (resumePill) { return resumePill; }
        var btn = document.createElement("button");
        btn.className = "wt-resume-pill";
        btn.textContent = "Resume Tour";
        btn.addEventListener("click", function () {
            resume();
        });
        document.body.appendChild(btn);
        resumePill = btn;
        return btn;
    }

    // -----------------------------------------------------------------------
    // Minimize / Resume / Skip
    // -----------------------------------------------------------------------

    function minimize() {
        if (driverInstance) {
            driverInstance.destroy();
            driverInstance = null;
        }
        isMinimized = true;
        sessionStorage.setItem("wt-current-step", String(currentStep));
        var pill = createResumePill();
        pill.classList.add("visible");
    }

    function resume() {
        isMinimized = false;
        if (resumePill) {
            resumePill.classList.remove("visible");
        }
        var saved = sessionStorage.getItem("wt-current-step");
        if (saved !== null) {
            currentStep = parseInt(saved, 10) || 0;
        }
        startTourAtStep(currentStep);
    }

    function skipToEnd() {
        if (driverInstance) {
            driverInstance.destroy();
            driverInstance = null;
        }
        sessionStorage.removeItem("wt-current-step");
        showMakeYoursOverlay();
    }

    // -----------------------------------------------------------------------
    // Popover customization
    // -----------------------------------------------------------------------

    /**
     * onPopoverRender: inject step counter, minimize button, and
     * skip-to-end link into the Driver.js popover.
     */
    function onPopoverRender(popover, opts) {
        if (!popover || !popover.wrapper) { return; }

        var wrapper = popover.wrapper;

        // Step counter
        var counter = document.createElement("div");
        counter.className = "wt-step-counter";
        counter.textContent = "Step " + (currentStep + 1) + " of " + steps.length;

        var titleEl = wrapper.querySelector(".driver-popover-title");
        if (titleEl) {
            titleEl.parentNode.insertBefore(counter, titleEl);
        }

        // Minimize button
        var minBtn = document.createElement("button");
        minBtn.textContent = "\u2013";
        minBtn.title = "Minimize tour";
        minBtn.style.cssText =
            "position:absolute;top:8px;right:36px;background:none;" +
            "border:none;font-size:1.2rem;cursor:pointer;color:#9ca3af;" +
            "line-height:1;padding:2px 6px;";
        minBtn.addEventListener("click", function (e) {
            e.stopPropagation();
            minimize();
        });
        wrapper.style.position = "relative";
        wrapper.appendChild(minBtn);

        // Skip to end link
        var footerArea = wrapper.querySelector(".driver-popover-footer") ||
            wrapper.querySelector(".driver-popover-navigation-btns");
        if (footerArea) {
            var skip = document.createElement("span");
            skip.className = "wt-skip-link";
            skip.textContent = "Skip to end";
            skip.addEventListener("click", function (e) {
                e.stopPropagation();
                skipToEnd();
            });
            footerArea.appendChild(skip);
        }
    }

    // -----------------------------------------------------------------------
    // Navigation helpers: programmatic clicks
    // -----------------------------------------------------------------------

    /** Click the first server item in the cluster navigator. */
    function clickFirstServer() {
        var items = document.querySelectorAll(".server-item-row");
        if (items.length > 0) {
            items[0].click();
        }
    }

    /** Open the admin panel by clicking the settings button. */
    function openAdminPanel() {
        var btn = document.querySelector('[aria-label="open administration"]');
        if (btn) { btn.click(); }
    }

    /** Close the admin panel by clicking its close button. */
    function closeAdminPanel() {
        var btn = document.querySelector('[aria-label="close administration"]');
        if (btn) { btn.click(); }
    }

    /** Click a nav item in the admin sidebar by its label text. */
    function clickAdminNavItem(label) {
        var items = document.querySelectorAll(".MuiDialog-root .MuiListItemButton-root");
        for (var i = 0; i < items.length; i++) {
            var text = items[i].textContent.trim();
            if (text === label) {
                items[i].click();
                return;
            }
        }
    }

    /** Open the Ask Ellie chat panel by clicking the FAB. */
    function openChatPanel() {
        var fab = document.querySelector('[aria-label="open chat"]');
        if (fab) { fab.click(); }
    }

    // -----------------------------------------------------------------------
    // Step definitions
    // -----------------------------------------------------------------------

    var steps = [

        // -------------------------------------------------------------------
        // Part 1: The Big Picture (steps 0-2)
        // -------------------------------------------------------------------

        // Step 0 — Estate Dashboard
        {
            popover: {
                title: "Welcome to AI DBA Workbench",
                description:
                    "This is the estate dashboard. It shows every PostgreSQL " +
                    "server under management, organized into clusters and " +
                    "groups. The left panel is the navigator; the right panel " +
                    "shows status details for whatever you select.",
                side: "over",
                align: "center",
            },
        },

        // Step 1 — AI Overview
        {
            element: ".MuiPaper-root:has(> .MuiBox-root > svg[data-testid='AutoAwesomeIcon'])",
            popover: {
                title: "AI Overview",
                description: aiDesc(
                    "The AI Overview summarizes the current state of your " +
                    "estate in plain language. It updates automatically as " +
                    "conditions change, highlighting issues that need attention.",
                    "ai-overview.png"
                ),
                side: "bottom",
                align: "start",
            },
            onHighlightStarted: function () {
                // Ensure estate view is selected so AI Overview is visible
                var estateRow = document.querySelector('[title="View estate overview"]');
                if (!estateRow) {
                    // Try the tooltip wrapper
                    var tooltips = document.querySelectorAll(".MuiTooltip-tooltip");
                    tooltips.forEach(function (t) {
                        if (t.textContent.includes("estate")) {
                            var parent = t.closest("[role='button']");
                            if (parent) parent.click();
                        }
                    });
                }
            },
        },

        // Step 2 — Event Timeline
        {
            element: ".MuiBox-root:has(> .MuiBox-root > svg[data-testid='TimelineIcon'])",
            popover: {
                title: "Event Timeline",
                description:
                    "The event timeline displays a chronological view of " +
                    "configuration changes, restarts, and anomalies across " +
                    "your servers. Click any marker to see event details.",
                side: "bottom",
                align: "start",
            },
        },

        // -------------------------------------------------------------------
        // Part 2: Diagnosing a Problem (steps 3-9)
        // -------------------------------------------------------------------

        // Step 3 — Click into a server
        {
            element: ".server-item-row",
            popover: {
                title: "Select a Server",
                description:
                    "Click a server in the navigator to view its detailed " +
                    "status. The panel updates to show that server's alerts, " +
                    "metrics, and monitoring dashboards.",
                side: "right",
                align: "start",
            },
            onHighlightStarted: function () {
                clickFirstServer();
            },
        },

        // Step 4 — Top Queries
        {
            element: "[aria-label='Collapse Top Queries section'], [aria-label='Expand Top Queries section']",
            popover: {
                title: "Top Queries",
                description:
                    "The top queries section shows the most resource-intensive " +
                    "queries on this server, ranked by total execution time. " +
                    "Click any query to see its full text and execution plan.",
                side: "top",
                align: "start",
            },
            onHighlightStarted: function () {
                // Ensure the monitoring section is expanded
                var monitoringBtn = document.querySelector(
                    "[aria-label='Expand Monitoring section']"
                );
                if (monitoringBtn) { monitoringBtn.click(); }
            },
        },

        // Step 5 — Chart AI Analysis brain icon on a KPI tile
        {
            element: ".MuiPaper-root .MuiIconButton-root:has(svg[data-testid='PsychologyIcon'])",
            popover: {
                title: "Chart AI Analysis",
                description: aiDesc(
                    "Every metric chart has a brain icon. Click it to get an " +
                    "AI-powered analysis of the metric trend, including " +
                    "anomaly detection and recommendations.",
                    "chart-analysis.png"
                ),
                side: "bottom",
                align: "start",
            },
        },

        // Step 6 — Database drill-down
        {
            element: "[aria-label='Collapse Database Summaries section'], [aria-label='Expand Database Summaries section']",
            popover: {
                title: "Database Drill-Down",
                description:
                    "The Database Summaries section shows each database on " +
                    "this server with key metrics. Click a database card to " +
                    "drill down into table sizes, index usage, cache hit " +
                    "ratios, and more.",
                side: "top",
                align: "start",
            },
        },

        // Step 7 — Object drill-down
        {
            element: "[aria-label='Collapse Table Leaderboard section'], [aria-label='Expand Table Leaderboard section']",
            popover: {
                title: "Object Drill-Down",
                description:
                    "After drilling into a database, the Table Leaderboard " +
                    "ranks tables by activity. Click any table to inspect " +
                    "sequential scan counts, dead tuple ratios, and " +
                    "index efficiency.",
                side: "top",
                align: "start",
            },
        },

        // Step 8 — Alerts Section
        {
            element: ".MuiBox-root:has(> .MuiBox-root > svg[data-testid='NotificationsActiveIcon'])",
            popover: {
                title: "Alert Details",
                description: aiDesc(
                    "Each alert shows severity, threshold values, and timing. " +
                    "Click the brain icon on any alert to request an AI " +
                    "analysis that explains the root cause and suggests " +
                    "remediation steps.",
                    "alert-analysis.png"
                ),
                side: "top",
                align: "start",
            },
        },

        // Step 9 — Server Analysis button
        {
            element: "[aria-label='Run full analysis']",
            popover: {
                title: "Full Server Analysis",
                description: aiDesc(
                    "The brain icon next to the AI Overview triggers a " +
                    "comprehensive server analysis. The AI examines metrics, " +
                    "active alerts, query patterns, and replication status " +
                    "to produce a detailed health report.",
                    "server-analysis.png"
                ),
                side: "bottom",
                align: "start",
            },
        },

        // -------------------------------------------------------------------
        // Part 3: Ask Ellie (steps 10-13)
        // -------------------------------------------------------------------

        // Step 10 — Chat toggle button (FAB)
        {
            element: '[aria-label="open chat"]',
            popover: {
                title: "Meet Ellie",
                description: aiDesc(
                    "Ellie is the AI assistant built into the workbench. " +
                    "Click this button to open the chat panel and start " +
                    "a conversation about your databases.",
                    "ellie-fab.png"
                ),
                side: "left",
                align: "end",
            },
            onHighlightStarted: function () {
                // Close chat if already open so we can highlight the FAB
                var closeBtn = document.querySelector('[aria-label="Close chat panel"]');
                if (closeBtn) { closeBtn.click(); }
            },
        },

        // Step 11 — Chat input
        {
            element: '[aria-label="AI Chat Panel"]',
            popover: {
                title: "Ask Anything",
                description: aiDesc(
                    "Type a question in natural language. For example: " +
                    '"Why is CPU usage high on my primary server?" ' +
                    "Ellie can query metrics, run diagnostics, and explain " +
                    "what she finds.",
                    "ellie-chat.png"
                ),
                side: "left",
                align: "start",
            },
            onHighlightStarted: function () {
                openChatPanel();
            },
        },

        // Step 12 — Run in Database button
        {
            element: '[aria-label="AI Chat Panel"]',
            popover: {
                title: "Run SQL from Chat",
                description: aiDesc(
                    "When Ellie suggests SQL queries, a \"Run in Database\" " +
                    "button appears on each code block. Click it to execute " +
                    "the query directly and see the results inline.",
                    "run-in-db.png"
                ),
                side: "left",
                align: "center",
            },
        },

        // Step 13 — Follow-up query suggestion
        {
            element: '[aria-label="Chat message input"]',
            popover: {
                title: "Follow-Up Questions",
                description:
                    "Ellie remembers the conversation context. Ask follow-up " +
                    "questions to dig deeper. Try: \"What indexes would help?\" " +
                    "or \"Show me the slow queries from the last hour.\"",
                side: "top",
                align: "start",
            },
        },

        // -------------------------------------------------------------------
        // Part 4: How It's Configured (steps 14-19)
        // -------------------------------------------------------------------

        // Step 14 — Admin panel: Connections / Probe Defaults
        {
            element: ".MuiDialog-root",
            popover: {
                title: "Probe Defaults",
                description:
                    "The Probe Defaults page controls which metrics are " +
                    "collected and how frequently. Each probe can be enabled, " +
                    "disabled, or have its polling interval adjusted.",
                side: "left",
                align: "start",
            },
            onHighlightStarted: function () {
                openAdminPanel();
                setTimeout(function () {
                    clickAdminNavItem("Probe Defaults");
                }, 300);
            },
        },

        // Step 15 — Probes
        {
            element: ".MuiDialog-root",
            popover: {
                title: "Alert Defaults",
                description:
                    "Alert Defaults define the built-in thresholds for each " +
                    "metric. When a metric crosses its threshold, the system " +
                    "fires an alert. You can customize thresholds and " +
                    "severities here.",
                side: "left",
                align: "start",
            },
            onHighlightStarted: function () {
                clickAdminNavItem("Alert Defaults");
            },
        },

        // Step 16 — Alert Rules
        {
            element: ".MuiDialog-root",
            popover: {
                title: "Email Notification Channels",
                description:
                    "Notification channels control where alerts are delivered. " +
                    "Email channels send formatted alert messages to the " +
                    "configured recipients. You can also set up Slack, " +
                    "Mattermost, and webhook channels.",
                side: "left",
                align: "start",
            },
            onHighlightStarted: function () {
                clickAdminNavItem("Email Channels");
            },
        },

        // Step 17 — Close admin, show Alert Overrides
        {
            popover: {
                title: "Alert Overrides",
                description:
                    "Back on the main panel, individual servers can have " +
                    "alert overrides. These let you raise, lower, or disable " +
                    "thresholds for a specific connection without changing " +
                    "the global defaults.",
                side: "over",
                align: "center",
            },
            onHighlightStarted: function () {
                closeAdminPanel();
            },
        },

        // Step 18 — Reopen admin: Notification Channels
        {
            element: ".MuiDialog-root",
            popover: {
                title: "Slack Channels",
                description:
                    "Slack channels deliver alert notifications to your " +
                    "team's Slack workspace. Configure the webhook URL and " +
                    "choose which alert severities trigger a message.",
                side: "left",
                align: "start",
            },
            onHighlightStarted: function () {
                openAdminPanel();
                setTimeout(function () {
                    clickAdminNavItem("Slack Channels");
                }, 300);
            },
        },

        // Step 19 — Blackout Management
        {
            element: ".MuiDialog-root",
            popover: {
                title: "Blackout Windows",
                description:
                    "Sometimes you need to silence alerts during planned " +
                    "maintenance. Blackout windows suppress notifications " +
                    "at the estate, group, cluster, or server level for a " +
                    "specified period.",
                side: "left",
                align: "start",
            },
            onHighlightStarted: function () {
                closeAdminPanel();
                // Open blackout management from the status panel header.
                // The blackout icon is DarkModeIcon inside a Badge. Find
                // all DarkModeIcon SVGs and pick the one inside a Badge
                // (which distinguishes it from the theme toggle in the header).
                setTimeout(function () {
                    var icons = document.querySelectorAll(
                        'svg[data-testid="DarkModeIcon"]'
                    );
                    for (var i = 0; i < icons.length; i++) {
                        if (icons[i].closest(".MuiBadge-root")) {
                            var btn = icons[i].closest("button");
                            if (btn) { btn.click(); break; }
                        }
                    }
                }, 400);
            },
        },

        // -------------------------------------------------------------------
        // Part 5: Who Can Access What (steps 20-22)
        // -------------------------------------------------------------------

        // Step 20 — Admin: Users
        {
            element: ".MuiDialog-root",
            popover: {
                title: "User Management",
                description:
                    "The Users page lets administrators create and manage " +
                    "user accounts. Each user can be assigned to groups that " +
                    "control their access to connections and features.",
                side: "left",
                align: "start",
            },
            onHighlightStarted: function () {
                // Close the blackout management dialog first
                var dialogs = document.querySelectorAll(".MuiDialog-root");
                dialogs.forEach(function (d) {
                    var title = d.querySelector(".MuiDialogTitle-root");
                    if (title && title.textContent.includes("Blackout")) {
                        var btn = d.querySelector('[aria-label="close"]');
                        if (btn) { btn.click(); }
                    }
                });
                setTimeout(function () {
                    openAdminPanel();
                    setTimeout(function () {
                        clickAdminNavItem("Users");
                    }, 300);
                }, 300);
            },
        },

        // Step 21 — Admin: Tokens
        {
            element: ".MuiDialog-root",
            popover: {
                title: "API Tokens",
                description:
                    "Tokens provide programmatic access to the workbench " +
                    "API. Each token has scoped permissions so you can " +
                    "grant exactly the access that automation scripts and " +
                    "integrations need.",
                side: "left",
                align: "start",
            },
            onHighlightStarted: function () {
                clickAdminNavItem("Tokens");
            },
        },

        // Step 22 — Admin: AI Memories
        {
            element: ".MuiDialog-root",
            popover: {
                title: "AI Memories",
                description: aiDesc(
                    "AI Memories are facts that Ellie remembers between " +
                    "conversations. She learns about your environment over " +
                    "time: server roles, maintenance windows, team " +
                    "preferences. You can review and edit these memories here.",
                    "ai-memories.png"
                ),
                side: "left",
                align: "start",
            },
            onHighlightStarted: function () {
                clickAdminNavItem("Memories");
            },
        },
    ];

    // Step 23 is the Make It Yours overlay, not a Driver.js step.

    // -----------------------------------------------------------------------
    // Build the Driver.js configuration and start the tour
    // -----------------------------------------------------------------------

    function buildDriverConfig(startIndex) {
        return {
            showProgress: false,
            showButtons: ["next", "previous"],
            steps: steps.map(function (step, i) {
                return {
                    element: step.element || undefined,
                    popover: {
                        title: step.popover.title,
                        description: step.popover.description,
                        side: step.popover.side || "bottom",
                        align: step.popover.align || "start",
                        onPopoverRender: function (popover, opts) {
                            currentStep = i;
                            onPopoverRender(popover, opts);
                        },
                    },
                    onHighlightStarted: step.onHighlightStarted || undefined,
                };
            }),
            onDestroyStarted: function () {
                if (!driverInstance) { return; }
                // Always allow the destroy to proceed.
                driverInstance.destroy();
                driverInstance = null;
                // When on the last Driver.js step, show Make It Yours.
                if (currentStep >= steps.length - 1) {
                    showMakeYoursOverlay();
                }
            },
            onNextClick: function () {
                // On the last step, trigger Make It Yours
                if (currentStep >= steps.length - 1) {
                    if (driverInstance) {
                        driverInstance.destroy();
                        driverInstance = null;
                    }
                    showMakeYoursOverlay();
                    return;
                }
                if (driverInstance) {
                    driverInstance.moveNext();
                }
            },
            onPrevClick: function () {
                if (driverInstance) {
                    driverInstance.movePrevious();
                }
            },
        };
    }

    function startTourAtStep(stepIndex) {
        if (driverInstance) {
            driverInstance.destroy();
            driverInstance = null;
        }

        var driverFn = window.driver && window.driver.js && window.driver.js.driver;
        if (!driverFn) {
            console.error("Driver.js not loaded");
            return;
        }

        var config = buildDriverConfig(stepIndex);
        driverInstance = driverFn(config);

        // Driver.js calls onHighlightStarted automatically when
        // rendering a step. No need to call it manually here.
        driverInstance.drive(stepIndex);
    }

    // -----------------------------------------------------------------------
    // Initialization
    // -----------------------------------------------------------------------

    function init() {
        // Check for a previously minimized tour
        var savedStep = sessionStorage.getItem("wt-current-step");
        if (savedStep !== null) {
            currentStep = parseInt(savedStep, 10) || 0;
            isMinimized = true;
            var pill = createResumePill();
            pill.classList.add("visible");
            return;
        }

        // Wait for the app to fully load (dashboard or login screen)
        waitForDashboard()
            .then(function () {
                return checkStatus();
            })
            .then(function () {
                startTourAtStep(0);
            })
            .catch(function (err) {
                console.warn("Walkthrough: could not start tour.", err);
            });
    }

    /**
     * Wait for the dashboard to be ready. The app renders a
     * login form first; once the user logs in, the main layout
     * with the ClusterNavigator appears.
     */
    function waitForDashboard() {
        return new Promise(function (resolve) {
            // If the dashboard is already visible, resolve immediately
            if (document.querySelector(".MuiAppBar-root") &&
                document.querySelector(".server-item-row, [aria-label='open chat']")) {
                resolve();
                return;
            }

            // Use MutationObserver to detect when the dashboard renders
            var observer = new MutationObserver(function (mutations, obs) {
                if (document.querySelector(".MuiAppBar-root") &&
                    document.querySelector(".server-item-row, [aria-label='open chat']")) {
                    obs.disconnect();
                    // Small extra delay for rendering to stabilize
                    setTimeout(resolve, 800);
                }
            });

            observer.observe(document.body, {
                childList: true,
                subtree: true,
            });

            // Fallback timeout: if dashboard never appears, try anyway
            setTimeout(function () {
                observer.disconnect();
                resolve();
            }, 30000);
        });
    }

    // Start
    init();
})();
