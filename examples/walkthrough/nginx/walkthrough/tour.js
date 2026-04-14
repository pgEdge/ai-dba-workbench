/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
/*
 * pgEdge AI DBA Workbench — Guided Tour
 *
 * Self-initializing tour definition using Driver.js (IIFE bundle).
 * Loaded by loader.js after the Driver.js library is available.
 *
 * Architecture:
 *   24 steps across 6 parts, plus helper functions for minimize,
 *   resume, skip-to-end, and Make It Yours overlay.
 *
 * DOM Selector Strategy:
 *   The React/MUI app does not emit data-testid attributes. Steps
 *   target elements via aria-label, MUI class names (.MuiAppBar-root,
 *   .MuiDialog-root, .MuiFab-root), semantic roles, and structural
 *   CSS selectors (.server-item-row, .cluster-header). Steps that
 *   cannot reliably target an element use a centered popover with
 *   no element property.
 */
(function () {
    "use strict";

    // -----------------------------------------------------------------------
    // State
    // -----------------------------------------------------------------------

    var apiKeyConfigured = false;
    var currentStep = 0;
    var isMinimized = false;
    var driverInstance = null;
    var resumePill = null;
    var destroying = false;

    // -----------------------------------------------------------------------
    // Utility helpers
    // -----------------------------------------------------------------------

    /** Wait for a selector to appear in the DOM (returns a Promise). */
    function waitForElement(selector, timeout) {
        timeout = timeout || 8000;
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

    /**
     * Mark a description as AI-dependent. Returns a marker object
     * that onPopoverRender resolves at display time based on the
     * current apiKeyConfigured state.
     */
    function aiDesc(normalText) {
        return { __aiDesc: true, normal: normalText };
    }

    /** Resolve an aiDesc marker to HTML at render time. */
    function resolveAiDesc(desc) {
        if (!desc || !desc.__aiDesc) { return desc; }
        if (apiKeyConfigured) { return desc.normal; }
        return desc.normal +
            '<br><br><div style="background:#f0f9ff;border:1px solid #0ea5e9;' +
            'border-radius:8px;padding:10px 14px;margin:4px 0;color:#0c4a6e;' +
            'font-size:0.9rem;">' +
            'This feature uses AI. To enable AI features, run ' +
            '<code>guide.sh</code> again from your terminal.' +
            '</div>';
    }

    // -----------------------------------------------------------------------
    // Detect AI availability from the DOM
    // -----------------------------------------------------------------------

    /**
     * Check whether AI features are configured by looking for the
     * Ellie chat FAB (aria-label="open chat") or the AI Overview
     * section in the DOM. These elements only render when the
     * server has a valid LLM provider configured.
     */
    function detectAiFromDom() {
        var chatFab = document.querySelector('[aria-label="open chat"]');
        var aiOverview = document.querySelector(
            '[aria-label="Collapse AI Overview"], [aria-label="Expand AI Overview"]'
        );
        apiKeyConfigured = !!(chatFab || aiOverview);
    }

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
            '  the demo data. What would you like to do next?</p>' +
            '  <button class="wt-choice-btn" id="wt-choice-connect">' +
            '    <strong>Add your own database</strong>' +
            '    Click the + button in the navigator to add your database ' +
            '    using the built-in Add Server dialog.' +
            '  </button>' +
            '  <button class="wt-choice-btn" id="wt-choice-explore">' +
            '    <strong>Keep exploring the demo</strong>' +
            '    Continue using the demo data on your own.' +
            '  </button>' +
            '  <button class="wt-choice-btn" id="wt-choice-dismiss">' +
            '    <strong>Clean up everything</strong>' +
            '    Stop and remove all walkthrough containers and data.' +
            '  </button>' +
            '</div>';
        document.body.appendChild(overlay);

        // Add your own database — close the overlay so they can use the UI
        overlay.querySelector("#wt-choice-connect").addEventListener("click", function () {
            sessionStorage.setItem("wt-tour-closed", "true");
            sessionStorage.removeItem("wt-current-step");
            overlay.remove();
        });

        // Keep exploring
        overlay.querySelector("#wt-choice-explore").addEventListener("click", function () {
            sessionStorage.removeItem("wt-current-step");
            overlay.remove();
        });

        // Clean up everything
        overlay.querySelector("#wt-choice-dismiss").addEventListener("click", function () {
            sessionStorage.setItem("wt-tour-closed", "true");
            sessionStorage.removeItem("wt-current-step");
            overlay.innerHTML =
                '<div class="wt-make-yours-card">' +
                '  <h2>Clean Up</h2>' +
                '  <p>Run the following command in your terminal to stop ' +
                '  and remove all walkthrough containers and data:</p>' +
                '  <pre style="background:#1e293b;color:#e2e8f0;' +
                '  padding:12px 16px;border-radius:8px;font-size:0.9rem;' +
                '  overflow-x:auto;">cd examples/walkthrough\n' +
                'docker compose down -v</pre>' +
                '  <button class="wt-choice-btn" id="wt-close-cleanup">' +
                '    <strong>Close</strong>' +
                '  </button>' +
                '</div>';
            overlay.querySelector("#wt-close-cleanup").addEventListener("click", function () {
                overlay.remove();
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

        // If this step has no element (centered popover), force
        // viewport-center positioning. Driver.js uses position:
        // relative with inset, which places it in document flow.
        var stepDef = steps[currentStep];
        if (stepDef && !stepDef.element) {
            wrapper.style.position = "fixed";
            wrapper.style.inset = "auto";
            wrapper.style.top = "50%";
            wrapper.style.left = "50%";
            wrapper.style.transform = "translate(-50%, -50%)";
            wrapper.style.zIndex = "100001";
        }

        // Resolve dynamic descriptions at render time
        if (stepDef && stepDef.popover && stepDef.popover.description) {
            var desc = stepDef.popover.description;
            var descEl = wrapper.querySelector(".driver-popover-description");

            // Welcome step: show API key status
            if (desc.__welcomeStep && descEl) {
                var html =
                    "This guided tour walks you through every major feature " +
                    "in about 15 minutes.<br><br>" +
                    "The workbench has three areas: a <strong>navigator</strong> " +
                    "on the left lists your database servers, the <strong>main " +
                    "panel</strong> in the center shows status and metrics, and " +
                    "an <strong>AI chat</strong> assistant is available at the " +
                    "bottom right.";
                if (!apiKeyConfigured) {
                    html += '<br><br><div style="background:#f0f9ff;border:1px solid #0ea5e9;' +
                        'border-radius:8px;padding:10px 14px;color:#0c4a6e;font-size:0.9rem;">' +
                        'The workbench has many built-in AI features to assist you, but you ' +
                        'don\u2019t need AI to find it useful. If you want to enable AI features, ' +
                        'run <code>guide.sh</code> again from your terminal after the tour.' +
                        '</div>';
                } else {
                    html += '<br><br><div style="background:#ecfdf5;border:1px solid #10b981;' +
                        'border-radius:8px;padding:10px 14px;color:#065f46;font-size:0.9rem;">' +
                        '\u2705 AI features are enabled. You\u2019ll see live AI analysis throughout the tour.' +
                        '</div>';
                }
                descEl.innerHTML = html;
            }

            // AI-dependent steps: show gentle note if no key
            if (desc.__aiDesc && descEl) {
                descEl.innerHTML = resolveAiDesc(desc);
            }
        }

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
            "position:absolute;top:8px;right:8px;" +
            "background:#f3f4f6;border:1px solid #d1d5db;" +
            "border-radius:4px;font-size:1rem;cursor:pointer;" +
            "color:#6b7280;line-height:1;padding:2px 8px;" +
            "z-index:1;font-weight:bold;";
        minBtn.addEventListener("click", function (e) {
            e.stopPropagation();
            minimize();
        });
        // Do NOT set wrapper.style.position here — it breaks
        // Driver.js element-anchored positioning. The minimize
        // button uses fixed positioning instead.
        wrapper.appendChild(minBtn);

        // Skip to end link — on its own line below nav buttons
        var footerArea = wrapper.querySelector(".driver-popover-footer") ||
            wrapper.querySelector(".driver-popover-navigation-btns");
        if (footerArea) {
            var skipRow = document.createElement("div");
            skipRow.style.cssText =
                "text-align:center;padding-top:8px;border-top:1px solid #e5e7eb;margin-top:8px;";
            var skip = document.createElement("span");
            skip.className = "wt-skip-link";
            skip.textContent = "Skip to end \u2192";
            skip.addEventListener("click", function (e) {
                e.stopPropagation();
                skipToEnd();
            });
            skipRow.appendChild(skip);
            footerArea.parentNode.insertBefore(skipRow, footerArea.nextSibling);
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

    /** Expand the first cluster in the navigator if collapsed. */
    function expandFirstCluster() {
        var clusterHeaders = document.querySelectorAll(".cluster-header");
        for (var i = 0; i < clusterHeaders.length; i++) {
            var header = clusterHeaders[i];
            // Check if this cluster is collapsed by looking for the
            // ChevronRight icon (CollapseIcon means collapsed state)
            var chevron = header.querySelector('[data-testid="ChevronRightIcon"]');
            if (chevron) {
                // Find the expand/collapse IconButton
                var expandBtn = chevron.closest(".MuiIconButton-root");
                if (expandBtn) {
                    expandBtn.click();
                    return;
                }
            }
        }
    }

    /** Click the estate summary row to show estate overview. */
    function clickEstateOverview() {
        // The estate summary row is inside a Tooltip with title
        // "View estate overview". Find the clickable Box inside it.
        var rows = document.querySelectorAll('[role="button"], [class*="MuiBox-root"]');
        // Look for the text that contains "online of" which is
        // the estate status row
        for (var i = 0; i < rows.length; i++) {
            var text = rows[i].textContent || "";
            if (text.match(/\d+ online of \d+ servers/)) {
                rows[i].click();
                return;
            }
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

    /** Close the Ask Ellie chat panel. */
    function closeChatPanel() {
        var btn = document.querySelector('[aria-label="Close chat panel"]');
        if (btn) { btn.click(); }
    }

    /** Open the blackout management dialog from the status panel header. */
    function openBlackoutDialog() {
        // The blackout icon is DarkModeIcon inside a Badge, which is
        // inside the SelectionHeader (not the theme toggle in the Header).
        // The MuiBadge-root wrapper distinguishes it from the header
        // theme toggle.
        var icons = document.querySelectorAll('svg[data-testid="DarkModeIcon"]');
        for (var i = 0; i < icons.length; i++) {
            if (icons[i].closest(".MuiBadge-root")) {
                var btn = icons[i].closest("button");
                if (btn) {
                    btn.click();
                    return;
                }
            }
        }
    }

    /** Close any open MUI Dialog by clicking its close button. */
    function closeAnyDialog() {
        var dialogs = document.querySelectorAll(".MuiDialog-root");
        dialogs.forEach(function (d) {
            // Try standard close buttons: aria-label="close" for
            // regular dialogs, aria-label="close administration"
            // for the admin panel.
            var closeBtn = d.querySelector('[aria-label="close"]') ||
                d.querySelector('[aria-label="close administration"]');
            if (closeBtn) {
                closeBtn.click();
            }
        });
    }

    // -----------------------------------------------------------------------
    // Step definitions — 24 steps across 6 parts
    //
    // Selector notes (from reading the React source):
    //   - ClusterNavigator: Box with bgcolor=background.paper,
    //     borderRight, contains header "Database Servers". The
    //     first child of the mainLayoutBody flex row.
    //   - StatusPanel: Box with PANEL_ROOT_SX (overflow:auto, flex:1,
    //     p:3). The second child of the content area.
    //   - Header: MuiAppBar-root (AppBar position=static elevation=0)
    //   - ChatFAB: Fab with aria-label="open chat"
    //   - ChatPanel: Box with role="complementary"
    //     and aria-label="AI Chat Panel"
    //   - AdminPanel: fullScreen Dialog (.MuiDialog-root)
    //   - CollapsibleSection: aria-label="Collapse X section"
    //     or "Expand X section"
    //   - AIOverview: Paper containing SparkleIcon (AutoAwesome)
    //     with the label "AI Overview"
    //   - EventTimeline: collapsed section with "Timeline" header
    //   - SelectionHeader: first Box child inside PANEL_ROOT_SX
    //   - server-item-row: className on each ServerItem Box
    //   - cluster-header: className on each ClusterItem header Box
    // -----------------------------------------------------------------------

    var steps = [

        // -------------------------------------------------------------------
        // Part 1: The Big Picture (steps 0-2)
        // -------------------------------------------------------------------

        // Step 0 — Welcome (centered popover, no element highlight)
        //
        // On initial load the StatusPanel shows an empty state.
        // Show a centered welcome that explains the layout AND
        // the AI key status. The description is built dynamically
        // in onPopoverRender so it reflects the live
        // apiKeyConfigured state.
        {
            popover: {
                title: "Welcome to AI DBA Workbench",
                description: { __welcomeStep: true },
                side: "over",
                align: "center",
            },
        },

        // Step 1 — Cluster Navigator panel
        //
        // The ClusterNavigator is the first flex child inside the
        // mainLayoutBody. It is a Box with bgcolor=background.paper and
        // borderRight. No stable ID; target the first child of the
        // layout body (after the AppBar) that contains the header
        // "Database Servers".
        {
            element: ".MuiAppBar-root ~ div > div:first-child",
            popover: {
                title: "The Navigator",
                description:
                    "The navigator organizes your PostgreSQL servers into " +
                    "groups and clusters. The demo environment includes a " +
                    "<strong>Demo Cluster</strong> with a " +
                    "<strong>demo-ecommerce</strong> server.<br><br>" +
                    "Click any server to view its dashboard. Click the " +
                    "estate summary row at the top to see an overview of " +
                    "all servers.",
                side: "right",
                align: "start",
            },
            onHighlightStarted: function () {
                // Ensure the first cluster is expanded so the server
                // items are visible inside the navigator.
                expandFirstCluster();
            },
        },

        // Step 2 — Click into a server (select demo-ecommerce)
        //
        // Target the first .server-item-row and programmatically
        // click it. The StatusPanel will load the server dashboard.
        {
            element: ".server-item-row",
            popover: {
                title: "Select a Server",
                description:
                    "Let's click the <strong>demo-ecommerce</strong> server " +
                    "to see its live dashboard. The main panel updates to " +
                    "show alerts, metrics, and monitoring charts for the " +
                    "selected server.",
                side: "right",
                align: "start",
            },
            onHighlightStarted: function () {
                // Ensure the cluster is expanded (in case the user
                // resumed the tour at this step)
                expandFirstCluster();
                // Give the Collapse animation time to reveal the
                // server rows before clicking
                setTimeout(function () {
                    clickFirstServer();
                }, 400);
            },
        },

        // -------------------------------------------------------------------
        // Part 2: Diagnosing a Problem (steps 3-9)
        // -------------------------------------------------------------------

        // Step 3 — AI Overview
        //
        // The AIOverview component renders a Paper when AI is active.
        // Target it via the collapse button aria-label. If not present
        // (no API key), Driver.js will show a centered popover
        // automatically since the element won't be found.
        {
            element: '[aria-label="Collapse AI Overview"], [aria-label="Expand AI Overview"]',
            popover: {
                title: "AI Overview",
                description: aiDesc(
                    "The AI Overview summarizes the current state of this " +
                    "server in plain language. It updates automatically as " +
                    "conditions change, highlighting issues that need attention."
                ),
                side: "bottom",
                align: "start",
            },
        },

        // Step 4 — Event Timeline
        //
        // The EventTimeline section is rendered inside the StatusPanel.
        // It does not use CollapsibleSection; it has its own header
        // component (TimelineHeader). Target the outer container.
        {
            element: '[aria-label="Collapse Monitoring section"], [aria-label="Expand Monitoring section"]',
            popover: {
                title: "Monitoring Dashboard",
                description:
                    "The monitoring section contains an event timeline and " +
                    "detailed metric charts. It shows system resources, " +
                    "PostgreSQL performance, WAL replication status, " +
                    "database summaries, and top queries.",
                side: "top",
                align: "start",
            },
        },

        // Step 5 — System Resources section
        //
        // Inside the Monitoring CollapsibleSection, the ServerDashboard
        // renders SystemResourcesSection as a CollapsibleSection with
        // title "System Resources".
        {
            element: '[aria-label="Collapse System Resources section"], [aria-label="Expand System Resources section"]',
            popover: {
                title: "System Resources",
                description:
                    "System resource charts show CPU usage, memory " +
                    "utilization, disk I/O, and network throughput. " +
                    "Each chart tracks real-time metrics collected from " +
                    "the server.",
                side: "top",
                align: "start",
            },
        },

        // Step 6 — Top Queries
        //
        // CollapsibleSection with title "Top Queries" inside
        // ServerDashboard > TopQueriesSection.
        {
            element: '[aria-label="Collapse Top Queries section"], [aria-label="Expand Top Queries section"]',
            popover: {
                title: "Top Queries",
                description:
                    "The top queries section shows the most resource-intensive " +
                    "queries on this server, ranked by total execution time. " +
                    "Click any query to see its full text and execution plan.",
                side: "top",
                align: "start",
            },
        },

        // Step 7 — Database Summaries section
        //
        // CollapsibleSection with title "Database Summaries" inside
        // ServerDashboard > DatabaseSummariesSection.
        {
            element: '[aria-label="Collapse Database Summaries section"], [aria-label="Expand Database Summaries section"]',
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

        // Step 8 — Alerts Section
        //
        // The AlertsSection renders inside the StatusPanel without a
        // unique aria-label. Use a centered popover to describe alerts.
        {
            popover: {
                title: "Alert Details",
                description: aiDesc(
                    "The Active Alerts section (above the monitoring " +
                    "charts) lists every alert for the selected server. " +
                    "Each alert shows severity, threshold values, and " +
                    "timing. Click the brain icon on any alert to request " +
                    "an AI analysis that explains the root cause and " +
                    "suggests remediation steps."
                ),
                side: "over",
                align: "center",
            },
        },

        // Step 9 — Full Server Analysis button
        //
        // The "Run full analysis" button (PsychologyIcon) appears in
        // the AIOverview header row, next to the "AI Overview" label.
        {
            element: '[aria-label="Run full analysis"]',
            popover: {
                title: "Full Server Analysis",
                description: aiDesc(
                    "The brain icon next to the AI Overview triggers a " +
                    "comprehensive server analysis. The AI examines metrics, " +
                    "active alerts, query patterns, and replication status " +
                    "to produce a detailed health report."
                ),
                side: "bottom",
                align: "start",
            },
        },

        // -------------------------------------------------------------------
        // Part 3: Ask Ellie (steps 10-13)
        // -------------------------------------------------------------------

        // Step 10 — Chat toggle button (FAB)
        //
        // The ChatFAB renders a Fab with aria-label="open chat" at
        // position:fixed bottom:24px right:24px. It is only rendered
        // when the chat panel is closed AND aiEnabled is true.
        {
            element: '[aria-label="open chat"]',
            popover: {
                title: "Meet Ellie",
                description: aiDesc(
                    "Ellie is the AI assistant built into the workbench. " +
                    "Click this button to open the chat panel and start " +
                    "a conversation about your databases."
                ),
                side: "left",
                align: "end",
            },
            onHighlightStarted: function () {
                // Close chat if already open so we can highlight the FAB
                closeChatPanel();
            },
        },

        // Step 11 — Chat panel open
        //
        // The ChatPanel is a Box with role="complementary" and
        // aria-label="AI Chat Panel". Open the panel and highlight it.
        {
            element: '[aria-label="AI Chat Panel"]',
            popover: {
                title: "Ask Anything",
                description: aiDesc(
                    "Type a question in natural language. For example: " +
                    '"Why is CPU usage high on my primary server?" ' +
                    "Ellie can query metrics, run diagnostics, and explain " +
                    "what she finds."
                ),
                side: "left",
                align: "start",
            },
            onHighlightStarted: function () {
                openChatPanel();
            },
        },

        // Step 12 — Run in Database button (in chat)
        //
        // Highlight the chat panel again with a different description.
        {
            element: '[aria-label="AI Chat Panel"]',
            popover: {
                title: "Run SQL from Chat",
                description: aiDesc(
                    "When Ellie suggests SQL queries, a \"Run in Database\" " +
                    "button appears on each code block. Click it to execute " +
                    "the query directly and see the results inline."
                ),
                side: "left",
                align: "center",
            },
        },

        // Step 13 — Chat input field
        //
        // The ChatInput renders a TextField with
        // aria-label="Chat message input".
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
            onDeselected: function () {
                // Close the chat panel when leaving the Ellie section
                closeChatPanel();
            },
        },

        // -------------------------------------------------------------------
        // Part 4: How It's Configured (steps 14-19)
        // -------------------------------------------------------------------

        // Step 14 — Admin panel: Probe Defaults
        //
        // The AdminPanel is a fullScreen Dialog. Open it and navigate
        // to the Probe Defaults page.
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
                closeChatPanel();
                openAdminPanel();
                setTimeout(function () {
                    clickAdminNavItem("Probe Defaults");
                }, 400);
            },
        },

        // Step 15 — Admin panel: Alert Defaults
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

        // Step 16 — Admin panel: Email Channels
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

        // Step 17 — Close admin, explain Alert Overrides
        //
        // This is a centered popover (no element) that explains
        // alert overrides on the main panel.
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

        // Step 18 — Admin panel: Slack Channels
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
                }, 400);
            },
        },

        // Step 19 — Blackout Windows
        //
        // The BlackoutManagementDialog is a standard MUI Dialog opened
        // from the SelectionHeader's blackout icon button.
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
                // Wait for the fullScreen admin Dialog slide-out
                // animation to complete before opening the blackout
                // management dialog.
                setTimeout(function () {
                    openBlackoutDialog();
                }, 700);
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
                // Close any open dialogs (blackout dialog from step 19)
                closeAnyDialog();
                setTimeout(function () {
                    openAdminPanel();
                    setTimeout(function () {
                        clickAdminNavItem("Users");
                    }, 400);
                }, 400);
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
                    "preferences. You can review and edit these memories here."
                ),
                side: "left",
                align: "start",
            },
            onHighlightStarted: function () {
                clickAdminNavItem("Memories");
            },
        },

        // -------------------------------------------------------------------
        // Part 6: Wrap Up (step 23)
        // -------------------------------------------------------------------

        // Step 23 — Tour complete (centered popover)
        //
        // Close the admin panel and present a summary before showing
        // the Make It Yours overlay.
        {
            popover: {
                title: "Tour Complete",
                description:
                    "You have seen the major features of the AI DBA " +
                    "Workbench: real-time monitoring, AI-powered analysis, " +
                    "Ask Ellie, administration, alerts, and access control." +
                    "<br><br>Click <strong>Next</strong> to choose your " +
                    "next step: add your own database, keep exploring " +
                    "the demo, or clean up.",
                side: "over",
                align: "center",
            },
            onHighlightStarted: function () {
                closeAdminPanel();
            },
        },
    ];

    // -----------------------------------------------------------------------
    // Build the Driver.js configuration and start the tour
    // -----------------------------------------------------------------------

    function buildDriverConfig(startIndex) {
        return {
            showProgress: false,
            showButtons: ["next", "previous"],
            animate: true,
            overlayOpacity: 0.5,
            stagePadding: 8,
            stageRadius: 8,
            allowClose: false,
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
                    onDeselected: step.onDeselected || undefined,
                };
            }),
            onDestroyStarted: function () {
                if (destroying) { return; }
                destroying = true;
                driverInstance.destroy();
                driverInstance = null;
                // When on the last Driver.js step, show Make It Yours.
                if (currentStep >= steps.length - 1) {
                    showMakeYoursOverlay();
                }
                destroying = false;
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
                detectAiFromDom();
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
     *
     * Detection strategy:
     *   - MuiAppBar-root indicates the main layout header is
     *     rendered (login page uses a different theme and layout).
     *   - .server-item-row or [aria-label="open chat"] indicates
     *     cluster data has loaded or AI capabilities are active.
     */
    function waitForDashboard() {
        return new Promise(function (resolve, reject) {
            var TIMEOUT_MS = 5 * 60 * 1000; // 5 minutes

            function isDashboardReady() {
                // The main app bar only renders after successful login.
                // The login page does NOT have MuiAppBar-root.
                return document.querySelector(".MuiAppBar-root") != null;
            }

            if (isDashboardReady()) {
                setTimeout(resolve, 1000);
                return;
            }

            // Pre-fill login credentials if the login form is visible.
            // Uses the React-compatible nativeInputValueSetter pattern
            // so React's controlled input state picks up the values.
            var loginFilled = false;
            function prefillLogin() {
                if (loginFilled) { return; }
                var userInput = document.querySelector('input[type="text"]');
                var passInput = document.querySelector('input[type="password"]');
                if (userInput && passInput) {
                    var setter = Object.getOwnPropertyDescriptor(
                        window.HTMLInputElement.prototype, "value").set;
                    setter.call(userInput, "admin");
                    userInput.dispatchEvent(new Event("input", { bubbles: true }));
                    setter.call(passInput, "DemoPass2026");
                    passInput.dispatchEvent(new Event("input", { bubbles: true }));
                    loginFilled = true;
                }
            }

            // Poll every second until the dashboard renders.
            var elapsed = 0;
            var interval = setInterval(function () {
                elapsed += 1000;
                prefillLogin();
                if (isDashboardReady()) {
                    clearInterval(interval);
                    setTimeout(resolve, 1000);
                } else if (elapsed >= TIMEOUT_MS) {
                    clearInterval(interval);
                    console.warn("Walkthrough: timed out waiting for dashboard after 5 minutes.");
                    reject(new Error("Dashboard did not appear within 5 minutes"));
                }
            }, 1000);
        });
    }

    // Start
    init();
})();
