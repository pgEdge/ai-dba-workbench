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
 *   33 steps (indices 0-32) across 7 parts, plus helper functions
 *   for minimize, resume, skip-to-end, and Make It Yours overlay.
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
    var reHighlighting = false; // guard for moveTo re-evaluation

    // -----------------------------------------------------------------------
    // Utility helpers
    // -----------------------------------------------------------------------

    /**
     * Scroll an element into view within the StatusPanel.
     * The StatusPanel uses overflow:auto, so window.scrollIntoView
     * doesn't work — Driver.js positions highlights relative to
     * the viewport but the element scrolls inside the panel.
     * This finds the scroll container and sets scrollTop directly.
     */
    function scrollPanelTo(el) {
        if (!el) { return; }
        // Walk up from the element to find the overflow:auto container
        var container = el.parentElement;
        while (container) {
            var style = getComputedStyle(container);
            if (style.overflow === "auto" || style.overflowY === "auto" ||
                style.overflow === "scroll" || style.overflowY === "scroll") {
                // Found the scroll container — scroll so the element
                // is near the top of the visible area
                var elRect = el.getBoundingClientRect();
                var containerRect = container.getBoundingClientRect();
                var offset = elRect.top - containerRect.top + container.scrollTop;
                container.scrollTop = offset - 20;
                return;
            }
            container = container.parentElement;
        }
        // Fallback: no scroll container found, try regular scroll
        scrollPanelTo(el);
    }

    /**
     * Tag dynamic elements with data attributes so Driver.js
     * can target them. Called once after the dashboard loads.
     */
    function tagDynamicElements() {
        var ps = document.querySelectorAll("p");
        for (var i = 0; i < ps.length; i++) {
            var text = ps[i].textContent;

            // Tag the Event Timeline container
            // p -> left-side wrapper -> TimelineHeader wrapper -> outer Box
            if (text === "Event Timeline") {
                var container = ps[i].parentElement;
                if (container && container.parentElement && container.parentElement.parentElement) {
                    container.parentElement.parentElement.setAttribute("data-wt", "event-timeline");
                }
            }

            // Tag the Active Alerts section
            // p -> header div -> outer container
            if (text === "Active Alerts") {
                var alertContainer = ps[i].parentElement;
                if (alertContainer && alertContainer.parentElement) {
                    alertContainer.parentElement.setAttribute("data-wt", "active-alerts");
                }
            }

            // Tag the server info section (HOST label)
            // DOM: p(HOST) -> column div -> grid div -> outer div
            // From your Chrome DOM extract: div.css-1erjwy0 is the outer
            if (text === "HOST") {
                var col = ps[i].parentElement;       // column div
                var grid = col ? col.parentElement : null;  // grid div
                var outer = grid ? grid.parentElement : null; // outer div
                if (outer) {
                    outer.setAttribute("data-wt", "server-info");
                    outer.id = "wt-server-info";
                }
            }
        }
    }

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
            '    Open the Add Server dialog and connect to your PostgreSQL database.' +
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

        // Add your own database — open the Add Server dialog
        overlay.querySelector("#wt-choice-connect").addEventListener("click", function () {
            sessionStorage.setItem("wt-tour-closed", "true");
            sessionStorage.removeItem("wt-current-step");
            overlay.remove();
            // Click the "Add server or group" button in the navigator
            var addBtn = document.querySelector('[aria-label="Add server or group"]');
            if (addBtn) {
                setTimeout(function () { addBtn.click(); }, 300);
            }
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
                '  <p>To stop and remove the walkthrough, re-run the setup ' +
                '  script and choose the clean-up option:</p>' +
                '  <pre style="background:#1e293b;color:#e2e8f0;' +
                '  padding:12px 16px;border-radius:8px;font-size:0.9rem;' +
                '  overflow-x:auto;">bash guide.sh</pre>' +
                '  <h3 style="margin-top:16px;color:#0d9488;">Learn More</h3>' +
                '  <p style="font-size:0.9rem;">' +
                '    <a href="https://github.com/pgEdge/ai-dba-workbench/blob/main/docs/getting-started/quick-start.md" ' +
                '       target="_blank" style="color:#0d9488;">Quick Start Guide on GitHub</a><br>' +
                '    <a href="https://docs.pgedge.com/ai-dba-toolkit" ' +
                '       target="_blank" style="color:#0d9488;">Full Documentation on docs.pgedge.com</a>' +
                '  </p>' +
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

        // Force viewport-center for steps with no element.
        // Apply to .driver-popover directly (not wrapper, which may
        // be a child). CSS !important overrides inline styles.
        var stepDef = steps[currentStep];
        var popoverEl = document.querySelector(".driver-popover");
        if (popoverEl) {
            popoverEl.classList.remove("wt-centered-popover");
            popoverEl.classList.remove("wt-bottom-left-popover");
            if (stepDef && stepDef._bottomLeft) {
                popoverEl.classList.add("wt-bottom-left-popover");
                document.body.classList.add("wt-no-overlay");
            } else {
                document.body.classList.remove("wt-no-overlay");
                if (stepDef && !stepDef.element) {
                    popoverEl.classList.add("wt-centered-popover");
                }
            }
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
                    "A database with known problems and several hours of " +
                    "pre-seeded runtime metrics are included for " +
                    "illustrative purposes.<br><br>" +
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

    /** Find an admin sidebar nav item by its label text. */
    function findAdminNavItem(label) {
        var items = document.querySelectorAll(".MuiListItemButton-root");
        for (var i = 0; i < items.length; i++) {
            if ((items[i].textContent || "").trim() === label) {
                return items[i];
            }
        }
        return null;
    }

    /** Highlight an admin sidebar nav item with a teal outline. */
    function highlightAdminNavItem(label) {
        // Clear any previous highlights
        var items = document.querySelectorAll(".MuiListItemButton-root");
        items.forEach(function (item) {
            item.style.outline = "";
            item.style.outlineOffset = "";
        });
        // Highlight the target
        var item = findAdminNavItem(label);
        if (item) {
            item.style.outline = "2px solid #0d9488";
            item.style.outlineOffset = "2px";
            item.style.borderRadius = "4px";
        }
    }

    /** Remove highlights from admin nav items and tab bars. */
    function clearAdminNavHighlight() {
        var items = document.querySelectorAll(".MuiListItemButton-root");
        items.forEach(function (item) {
            item.style.outline = "";
            item.style.outlineOffset = "";
        });
        var tabBar = document.querySelector('[role="tablist"]');
        if (tabBar) { tabBar.style.outline = ""; }
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
            // Find any close button — different dialogs use different
            // aria-labels: "close", "close administration",
            // "close edit group", etc.
            var closeBtn = d.querySelector('[aria-label^="close"]') ||
                d.querySelector('[data-testid="CloseIcon"]')?.closest("button");
            if (closeBtn) {
                closeBtn.click();
                return;
            }
            // Fallback: look for a Cancel text button (e.g., blackout schedule form)
            var btns = d.querySelectorAll("button");
            for (var i = 0; i < btns.length; i++) {
                var txt = (btns[i].innerText || btns[i].textContent || "").trim();
                if (txt === "Cancel") {
                    btns[i].click();
                    return;
                }
            }
        });
        // Also close dashboard overlays (query detail, etc.)
        var overlayClose = document.querySelector('[aria-label="Close overlay"]');
        if (overlayClose) { overlayClose.click(); }
    }

    /**
     * Re-evaluate the current step after a dialog opens.
     * Driver.js evaluates the element selector when the step
     * first activates — if the dialog isn't open yet, the
     * element isn't found. This calls moveTo(currentStep) to
     * re-evaluate, with a guard to prevent infinite loops.
     */
    function reHighlightCurrentStep(delayMs) {
        if (reHighlighting) { return; }
        reHighlighting = true;
        document.body.classList.add("wt-transitioning");
        setTimeout(function () {
            if (driverInstance) {
                driverInstance.moveTo(currentStep);
            }
            // Show the popover after re-evaluation
            document.body.classList.remove("wt-transitioning");
            setTimeout(function () { reHighlighting = false; }, 100);
        }, delayMs || 500);
    }

    // -----------------------------------------------------------------------
    // Step definitions — 33 steps (indices 0-32) across 7 parts
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
    //   - EventTimeline: tagged via data-wt="event-timeline"
    //   - ActiveAlerts: tagged via data-wt="active-alerts"
    //   - ServerInfo: tagged via data-wt="server-info"
    //   - SelectionHeader: first Box child inside PANEL_ROOT_SX
    //   - server-item-row: className on each ServerItem Box
    //   - cluster-header: className on each ClusterItem header Box
    // -----------------------------------------------------------------------

    /**
     * Returns true if the given step index is an admin-panel step
     * (any step that opens or expects the admin dialog to be open).
     * Steps 21-24 are configuration admin steps, 29-31 are access
     * control admin steps. Steps 25-28 (server settings, blackout)
     * are NOT admin steps.
     */
    function isAdminStep(idx) {
        return (idx >= 21 && idx <= 24) || (idx >= 29 && idx <= 31);
    }

    /**
     * Returns true if the given step index is a blackout-dialog step
     * (the blackout manager or create blackout schedule steps).
     */
    function isBlackoutStep(idx) {
        return idx === 27 || idx === 28;
    }

    /**
     * Find the StatusPanel scroll container (the overflow:auto element)
     * and reset its scroll position to the top.
     */
    function scrollPanelToTop() {
        // The StatusPanel is the overflow:auto sibling of the navigator.
        // Walk from a known panel child or search for the container.
        var panels = document.querySelectorAll("div");
        for (var i = 0; i < panels.length; i++) {
            var style = getComputedStyle(panels[i]);
            if ((style.overflow === "auto" || style.overflowY === "auto") &&
                panels[i].scrollHeight > panels[i].clientHeight &&
                panels[i].clientHeight > 200) {
                panels[i].scrollTop = 0;
                return;
            }
        }
    }

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
        // mainLayoutBody.
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
                    // Re-tag dynamic elements after the server
                    // dashboard renders (Event Timeline appears)
                    setTimeout(tagDynamicElements, 1000);
                }, 400);
            },
        },

        // -------------------------------------------------------------------
        // Part 2: Server Dashboard (steps 3-16)
        // -------------------------------------------------------------------

        // Step 3 — AI Overview
        //
        // The AIOverview component renders inside a MuiPaper-root that
        // contains the collapse button. Target the Paper itself so the
        // entire block is highlighted. If AI is off, the element won't
        // exist and Driver.js shows a centered popover.
        {
            element: '.MuiPaper-root:has([aria-label="Collapse AI Overview"], [aria-label="Expand AI Overview"])',
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
            onHighlightStarted: function () {
                var el = document.querySelector(
                    '.MuiPaper-root:has([aria-label="Collapse AI Overview"], [aria-label="Expand AI Overview"])'
                );
                scrollPanelTo(el);
            },
        },

        // Step 4 — Full Server Analysis button
        //
        // The "Run full analysis" button (PsychologyIcon) appears in
        // the AIOverview header row, next to the "AI Overview" label.
        {
            element: '[aria-label="Run full analysis"]',
            popover: {
                title: "Full Server Analysis",
                description: aiDesc(
                    "Click this button to request a comprehensive AI " +
                    "analysis of the entire server. The analysis examines " +
                    "metrics, alerts, configuration, and performance to " +
                    "provide actionable recommendations."
                ),
                side: "left",
                align: "start",
            },
            onHighlightStarted: function () {
                var el = document.querySelector('[aria-label="Run full analysis"]');
                scrollPanelTo(el);
            },
        },

        // Step 5 — Server Information
        //
        // The server info bar shows connection details. Tagged with
        // id="wt-server-info" by tagDynamicElements().
        {
            element: "#wt-server-info",
            popover: {
                title: "Server Information",
                description:
                    "The server information bar shows connection details: " +
                    "host, port, database, user, PostgreSQL version, " +
                    "operating system, and the server's role in its cluster.",
                side: "bottom",
                align: "start",
            },
            onHighlightStarted: function () {
                scrollPanelToTop();
            },
        },

        // Step 6 — Server Details (i) button
        {
            element: '[aria-label="Server details"]',
            popover: {
                title: "Server Details",
                description:
                    "Click this icon for detailed server information " +
                    "including PostgreSQL version, operating system, " +
                    "extensions, data directory, and configuration " +
                    "parameters.",
                side: "left",
                align: "start",
            },
        },

        // Step 7 — Event Timeline bar
        //
        // The EventTimeline has no aria-label. Tagged with
        // data-wt="event-timeline" by tagDynamicElements().
        {
            element: '[data-wt="event-timeline"]',
            popover: {
                title: "Event Timeline",
                description:
                    "The event timeline shows every alert and event as " +
                    "it happens \u2014 color-coded by severity. Hover " +
                    "over events to see details, or click to investigate. " +
                    "This is your first stop when something goes wrong.",
                side: "bottom",
                align: "start",
            },
            onHighlightStarted: function () {
                var el = document.querySelector('[data-wt="event-timeline"]');
                scrollPanelTo(el);
            },
        },

        // Step 8 — Active Alerts
        //
        // Tagged with data-wt="active-alerts" by tagDynamicElements().
        {
            element: '[data-wt="active-alerts"]',
            popover: {
                title: "Active Alerts",
                description:
                    "Active alerts show every threshold breach and anomaly " +
                    "detected on this server. Each alert displays severity, " +
                    "the metric value that triggered it, and when it fired. " +
                    "Click any alert to investigate.",
                side: "bottom",
                align: "start",
            },
            onHighlightStarted: function () {
                var el = document.querySelector('[data-wt="active-alerts"]');
                scrollPanelTo(el);
            },
        },

        // Step 9 — Analyze with AI button (first one)
        //
        // Multiple "Analyze with AI" buttons exist (one per alert).
        // Target the first one.
        {
            element: '[aria-label="Analyze with AI"]',
            popover: {
                title: "AI Alert Analysis",
                description: aiDesc(
                    "Click this button on any alert to get an AI-powered " +
                    "analysis that explains the root cause and suggests " +
                    "specific remediation steps."
                ),
                side: "left",
                align: "start",
            },
            onHighlightStarted: function () {
                var el = document.querySelector('[aria-label="Analyze with AI"]');
                scrollPanelTo(el);
            },
        },

        // Step 10 — Monitoring Dashboard
        //
        // The CollapsibleSection wrapping all metric charts.
        {
            element: '[aria-label="Collapse Monitoring section"], [aria-label="Expand Monitoring section"]',
            popover: {
                title: "Monitoring Dashboard",
                description:
                    "The monitoring section contains detailed metric " +
                    "charts organized by category. Scroll down to explore " +
                    "system resources, PostgreSQL performance, replication, " +
                    "database summaries, and query analysis.",
                side: "bottom",
                align: "start",
            },
            onHighlightStarted: function () {
                var el = document.querySelector(
                    '[aria-label="Collapse Monitoring section"]'
                ) || document.querySelector(
                    '[aria-label="Expand Monitoring section"]'
                );
                scrollPanelTo(el);
            },
        },

        // Step 11 — System Resources section
        {
            element: '[aria-label="Collapse System Resources section"], [aria-label="Expand System Resources section"]',
            popover: {
                title: "System Resources",
                description:
                    "System resource charts show CPU usage, memory " +
                    "utilization, disk I/O, and network throughput. " +
                    "Each chart tracks real-time metrics collected from " +
                    "the server.",
                side: "bottom",
                align: "start",
            },
            onHighlightStarted: function () {
                var el = document.querySelector(
                    '[aria-label="Collapse System Resources section"]'
                ) || document.querySelector(
                    '[aria-label="Expand System Resources section"]'
                );
                scrollPanelTo(el);
            },
        },

        // Step 12 — PostgreSQL Overview section
        {
            element: '[aria-label="Collapse PostgreSQL Overview section"], [aria-label="Expand PostgreSQL Overview section"]',
            popover: {
                title: "PostgreSQL Overview",
                description:
                    "The PostgreSQL overview shows database-level " +
                    "performance: cache hit ratio, transaction rates " +
                    "(commits vs rollbacks), active connections, and " +
                    "checkpoint activity.",
                side: "bottom",
                align: "start",
            },
            onHighlightStarted: function () {
                var el = document.querySelector(
                    '[aria-label="Collapse PostgreSQL Overview section"]'
                ) || document.querySelector(
                    '[aria-label="Expand PostgreSQL Overview section"]'
                );
                scrollPanelTo(el);
            },
        },

        // Step 13 — WAL and Replication section
        {
            element: '[aria-label="Collapse WAL and Replication section"], [aria-label="Expand WAL and Replication section"]',
            popover: {
                title: "WAL and Replication",
                description:
                    "Write-Ahead Log (WAL) metrics show write throughput " +
                    "and sync times. For replicated servers, this section " +
                    "also displays replication lag, slot status, and " +
                    "subscription health.",
                side: "bottom",
                align: "start",
            },
            onHighlightStarted: function () {
                var el = document.querySelector(
                    '[aria-label="Collapse WAL and Replication section"]'
                ) || document.querySelector(
                    '[aria-label="Expand WAL and Replication section"]'
                );
                scrollPanelTo(el);
            },
        },

        // Step 14 — Database Summaries section
        {
            element: '[aria-label="Collapse Database Summaries section"], [aria-label="Expand Database Summaries section"]',
            popover: {
                title: "Database Summaries",
                description:
                    "Each database on this server gets a summary card " +
                    "showing size, connection count, transaction rate, " +
                    "and cache performance. Click a database to drill " +
                    "down into table and index details.",
                side: "bottom",
                align: "start",
            },
            onHighlightStarted: function () {
                var el = document.querySelector(
                    '[aria-label="Collapse Database Summaries section"]'
                ) || document.querySelector(
                    '[aria-label="Expand Database Summaries section"]'
                );
                scrollPanelTo(el);
            },
        },

        // Step 15 — Top Queries section
        {
            element: '[aria-label="Collapse Top Queries section"], [aria-label="Expand Top Queries section"]',
            popover: {
                title: "Top Queries",
                description:
                    "The slowest queries ranked by total execution time " +
                    "from pg_stat_statements. Click any query to see its " +
                    "full text, execution statistics, and plan analysis.",
                side: "bottom",
                align: "start",
            },
            onHighlightStarted: function () {
                var el = document.querySelector(
                    '[aria-label="Collapse Top Queries section"]'
                ) || document.querySelector(
                    '[aria-label="Expand Top Queries section"]'
                );
                scrollPanelTo(el);
            },
        },

        // Step 16 — Query Details (bottom-left popover)
        //
        // Click into the first query row in the Top Queries section
        // to show query details. The popover explains what opened.
        // Uses _bottomLeft to avoid a grey overlay covering the
        // query detail panel.
        {
            _bottomLeft: true,
            popover: {
                title: "Query Details",
                description:
                    "Clicking a query row opens its full text, execution " +
                    "plan, and detailed statistics. This helps identify " +
                    "optimization opportunities like missing indexes or " +
                    "inefficient joins.",
                side: "over",
                align: "center",
            },
            onHighlightStarted: function () {
                // Click the first query row in Top Queries.
                // Query rows have role="button" and aria-label
                // starting with "View details for query:"
                var row = document.querySelector(
                    '[aria-label^="View details for query"]'
                );
                if (row) {
                    scrollPanelTo(row);
                    setTimeout(function () { row.click(); }, 300);
                }
            },
        },

        // -------------------------------------------------------------------
        // Part 3: Ask Ellie (steps 17-20)
        // -------------------------------------------------------------------

        // Step 17 — Chat toggle button (FAB)
        //
        // Close any open dialogs/overlays and return to the main
        // dashboard before showing the chat FAB.
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
                // Return to main dashboard first — close dialogs,
                // dashboard overlays (query detail), and chat panel
                closeAnyDialog();
                var closeOverlay = document.querySelector(
                    '[aria-label="Close overlay"]'
                );
                if (closeOverlay) { closeOverlay.click(); }
                closeChatPanel();
                scrollPanelToTop();
            },
        },

        // Step 18 — Chat panel open
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

        // Step 19 — Run in Database button (in chat)
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

        // Step 20 — Chat input field
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
        // Part 4: How It's Configured (steps 21-25)
        // -------------------------------------------------------------------

        // Step 21 — Admin panel: Probe Defaults
        //
        // Open admin, click nav item, then re-highlight so Driver.js
        // targets the selected nav item.
        {
            element: ".MuiListItemButton-root.Mui-selected",
            popover: {
                title: "Probe Defaults",
                description:
                    "This page controls which metrics are collected and " +
                    "how frequently. Each probe can be enabled, disabled, " +
                    "or have its polling interval adjusted.",
                side: "right",
                align: "start",
            },
            onHighlightStarted: function () {
                if (reHighlighting) { return; }
                closeChatPanel();
                closeAnyDialog();
                setTimeout(function () {
                    openAdminPanel();
                    setTimeout(function () {
                        clickAdminNavItem("Probe Defaults");
                        reHighlightCurrentStep(300);
                    }, 400);
                }, 300);
            },
        },

        // Step 22 — Admin panel: Alert Defaults
        {
            element: ".MuiListItemButton-root.Mui-selected",
            popover: {
                title: "Alert Defaults",
                description:
                    "These define the built-in thresholds for each " +
                    "metric. When a metric crosses its threshold, the system " +
                    "fires an alert. You can customize thresholds and " +
                    "severities here.",
                side: "right",
                align: "start",
            },
            onHighlightStarted: function () {
                if (reHighlighting) { return; }
                openAdminPanel();
                setTimeout(function () {
                    clickAdminNavItem("Alert Defaults");
                    reHighlightCurrentStep(300);
                }, 400);
            },
        },

        // Step 23 — Admin panel: Email Channels
        {
            element: ".MuiListItemButton-root.Mui-selected",
            popover: {
                title: "Email Notification Channels",
                description:
                    "Notification channels control where alerts are " +
                    "delivered. Email channels send formatted alert " +
                    "messages to configured recipients. You can also " +
                    "set up Slack, Mattermost, and webhook channels.",
                side: "right",
                align: "start",
            },
            onHighlightStarted: function () {
                if (reHighlighting) { return; }
                openAdminPanel();
                setTimeout(function () {
                    clickAdminNavItem("Email Channels");
                    reHighlightCurrentStep(300);
                }, 400);
            },
        },

        // Step 24 — Admin panel: Slack Channels
        {
            element: ".MuiListItemButton-root.Mui-selected",
            popover: {
                title: "Slack Channels",
                description:
                    "Slack channels deliver alert notifications to your " +
                    "team's workspace. Configure the webhook URL and choose " +
                    "which alert severities trigger a message.",
                side: "right",
                align: "start",
            },
            onHighlightStarted: function () {
                if (reHighlighting) { return; }
                openAdminPanel();
                setTimeout(function () {
                    clickAdminNavItem("Slack Channels");
                    reHighlightCurrentStep(300);
                }, 400);
            },
        },

        // Step 25 — Server Settings
        //
        // Close the admin panel, force the action buttons visible on
        // the server row, and click the settings icon to open the
        // server settings dialog. Popover sits bottom-left so it
        // does not cover the dialog. Targets the server-item-row's
        // action buttons specifically (not a group-level button).
        // A teal outline highlights the dialog's tab bar.
        {
            element: '[role="tablist"]',
            popover: {
                title: "Server Settings",
                description:
                    "Each server has its own settings for alert overrides, " +
                    "probe collection intervals, and notification channels. " +
                    "Changes here override the global defaults shown in the " +
                    "administration panel.",
                side: "bottom",
                align: "start",
            },
            onHighlightStarted: function () {
                if (reHighlighting) { return; }
                closeAdminPanel();
                var serverRow = document.querySelector(".server-item-row");
                if (serverRow) {
                    var actionBtns = serverRow.querySelector(".action-buttons");
                    if (actionBtns) {
                        actionBtns.style.opacity = "1";
                        var settingsBtn = actionBtns.querySelector(
                            ".MuiIconButton-root"
                        );
                        if (settingsBtn) {
                            setTimeout(function () {
                                settingsBtn.click();
                                reHighlightCurrentStep(500);
                            }, 500);
                        }
                    }
                }
            },
            // Note: no onDeselected here — closing the dialog is
            // handled by step 26's onHighlightStarted. We can't
            // close in onDeselected because reHighlightCurrentStep
            // triggers onDeselected on the same step.
        },

        // -------------------------------------------------------------------
        // Part 5: Blackout Windows (steps 26-28)
        // -------------------------------------------------------------------

        // Step 26 — Blackout Windows icon
        //
        // Close everything, return to dashboard. Target the blackout
        // management button (moon icon) in the status panel header.
        {
            element: '[aria-label="Blackout management"]',
            popover: {
                title: "Blackout Windows",
                description:
                    "This moon icon opens the blackout manager. Blackout " +
                    "windows are maintenance periods during which alerts " +
                    "are suppressed. They can be one-time or recurring, " +
                    "and scoped to specific servers, clusters, or the " +
                    "entire estate.",
                side: "bottom",
                align: "start",
            },
            onHighlightStarted: function () {
                closeAnyDialog();
                scrollPanelToTop();
            },
        },

        // Step 27 — Blackout Management dialog
        //
        // Open the blackout dialog, then re-highlight to target it.
        {
            element: '[role="dialog"]',
            popover: {
                title: "Blackout Manager",
                description:
                    "View active and scheduled blackouts. Use the buttons " +
                    "at the bottom to create one-time or recurring blackouts.",
                side: "right",
                align: "start",
            },
            onHighlightStarted: function () {
                if (reHighlighting) { return; }
                openBlackoutDialog();
                reHighlightCurrentStep(500);
            },
        },

        // Step 28 — Create Blackout Schedule
        //
        // Click "New Scheduled Blackout" to open a second dialog.
        // Tag the new dialog with an id so we target it (not the
        // first/smaller blackout manager dialog behind it).
        {
            element: '#wt-schedule-dialog',
            popover: {
                title: "Create Blackout Schedule",
                description:
                    "Define recurring maintenance windows with cron " +
                    "expressions. Each schedule specifies when alerts " +
                    "should be suppressed, for how long, and which " +
                    "servers are affected.",
                side: "right",
                align: "start",
            },
            onHighlightStarted: function () {
                if (reHighlighting) { return; }
                var dialog = document.querySelector(".MuiDialog-root");
                if (dialog) {
                    var target = null;
                    var btns = dialog.querySelectorAll("button");
                    for (var i = 0; i < btns.length; i++) {
                        var txt = (btns[i].innerText || btns[i].textContent || "").trim();
                        if (txt.indexOf("Scheduled Blackout") !== -1) {
                            target = btns[i];
                            break;
                        }
                    }
                    if (target) {
                        target.click();
                        // Wait for the new dialog, then tag it.
                        // Tag the LAST MuiDialog-paper (the schedule form).
                        // The blackout manager paper is behind it.
                        setTimeout(function () {
                            var papers = document.querySelectorAll(".MuiDialog-paper");
                            if (papers.length > 0) {
                                papers[papers.length - 1].id = "wt-schedule-dialog";
                            }
                            reHighlightCurrentStep(200);
                        }, 400);
                    }
                }
            },
            onDeselected: function () {
                // Skip cleanup during re-highlight (moveTo same step)
                if (reHighlighting) { return; }
                var dialog = document.querySelector(".MuiDialog-root");
                if (dialog) {
                    var btns = dialog.querySelectorAll("button");
                    for (var i = 0; i < btns.length; i++) {
                        var txt = (btns[i].innerText || btns[i].textContent || "").trim();
                        if (txt === "Cancel") {
                            btns[i].click();
                            break;
                        }
                    }
                }
                setTimeout(function () { closeAnyDialog(); }, 300);
            },
        },

        // -------------------------------------------------------------------
        // Part 6: Who Can Access What (steps 29-31)
        // -------------------------------------------------------------------

        // Step 29 — Admin: Users
        {
            element: ".MuiListItemButton-root.Mui-selected",
            popover: {
                title: "User Management",
                description:
                    "Create and manage user accounts. Each user can be " +
                    "assigned to groups that control their access to " +
                    "connections and features.",
                side: "right",
                align: "start",
            },
            onHighlightStarted: function () {
                if (reHighlighting) { return; }
                closeAnyDialog();
                setTimeout(function () {
                    openAdminPanel();
                    setTimeout(function () {
                        clickAdminNavItem("Users");
                        reHighlightCurrentStep(300);
                    }, 400);
                }, 400);
            },
        },

        // Step 30 — Admin: Tokens
        {
            element: ".MuiListItemButton-root.Mui-selected",
            popover: {
                title: "API Tokens",
                description:
                    "Tokens provide programmatic access to the workbench " +
                    "API. Each token has scoped permissions so you can " +
                    "grant exactly the access that automation scripts and " +
                    "integrations need.",
                side: "right",
                align: "start",
            },
            onHighlightStarted: function () {
                if (reHighlighting) { return; }
                openAdminPanel();
                setTimeout(function () {
                    clickAdminNavItem("Tokens");
                    reHighlightCurrentStep(300);
                }, 400);
            },
        },

        // Step 31 — Admin: AI Memories
        {
            element: ".MuiListItemButton-root.Mui-selected",
            popover: {
                title: "AI Memories",
                description: aiDesc(
                    "AI Memories are facts that Ellie remembers between " +
                    "conversations. She learns about your environment over " +
                    "time: server roles, maintenance windows, team " +
                    "preferences. Review and edit them here."
                ),
                side: "right",
                align: "start",
            },
            onHighlightStarted: function () {
                if (reHighlighting) { return; }
                openAdminPanel();
                setTimeout(function () {
                    clickAdminNavItem("Memories");
                    reHighlightCurrentStep(300);
                }, 400);
            },
        },

        // -------------------------------------------------------------------
        // Part 7: Wrap Up (step 32)
        // -------------------------------------------------------------------

        // Step 32 — Tour Complete (centered popover)
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
                // Re-tag dynamic elements before every step.
                tagDynamicElements();
                // Hide popover during transition for steps that will
                // re-highlight. Uses a body class so it persists
                // even when Driver.js recreates the popover element.
                var nextIdx = currentStep + 1;
                if (isAdminStep(nextIdx) || nextIdx === 25 ||
                    isBlackoutStep(nextIdx)) {
                    document.body.classList.add("wt-transitioning");
                }
                if (driverInstance) {
                    driverInstance.moveNext();
                }
            },
            onPrevClick: function () {
                var prevStep = currentStep - 1;
                if (prevStep >= 0) {
                    // Close query detail overlay when going back from
                    // Query Details (16) or Meet Ellie (17)
                    if (currentStep === 16 || currentStep === 17) {
                        var overlayClose = document.querySelector(
                            '[aria-label="Close overlay"]'
                        );
                        if (overlayClose) { overlayClose.click(); }
                    }

                    // Going back from Create Blackout Schedule (28)
                    // to Blackout Manager (27) — close everything and
                    // let step 27 rebuild from scratch.
                    if (currentStep === 28) {
                        closeAnyDialog();
                    }

                    // Going back from User Management (29) to Create
                    // Blackout Schedule (28) — close admin panel,
                    // re-open blackout dialog, click New Scheduled
                    // Blackout.
                    if (currentStep === 29) {
                        closeAdminPanel();
                        setTimeout(function () {
                            openBlackoutDialog();
                            setTimeout(function () {
                                var dlg = document.querySelector(
                                    ".MuiDialog-root"
                                );
                                if (dlg) {
                                    var dbtns = dlg.querySelectorAll("button");
                                    for (var j = 0; j < dbtns.length; j++) {
                                        var t = (dbtns[j].innerText ||
                                            dbtns[j].textContent || "").trim();
                                        if (t.indexOf("Scheduled Blackout") !== -1) {
                                            dbtns[j].click();
                                            break;
                                        }
                                    }
                                }
                            }, 500);
                        }, 400);
                    }

                    // Close dialogs when navigating backward from a
                    // dialog step to a non-dialog step. This covers
                    // both admin-panel steps and blackout steps.
                    var curIsDialog = isAdminStep(currentStep) ||
                        isBlackoutStep(currentStep) ||
                        currentStep === 25;
                    var prevIsDialog = isAdminStep(prevStep) ||
                        isBlackoutStep(prevStep) ||
                        prevStep === 25;
                    if (curIsDialog && !prevIsDialog) {
                        closeAnyDialog();
                    }
                }
                tagDynamicElements();
                // Hide popover during backward transitions to
                // dialog steps (same as forward transitions)
                if (isAdminStep(prevStep) || prevStep === 25 ||
                    isBlackoutStep(prevStep)) {
                    document.body.classList.add("wt-transitioning");
                }
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
                tagDynamicElements();
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
