/*
 * Walkthrough Loader
 *
 * Injected into every page via nginx sub_filter.
 * Dynamically loads Driver.js CSS and the tour definition.
 * Only activates once per browser session.
 */
(function () {
  "use strict";

  // Skip if tour was permanently dismissed
  if (localStorage.getItem("wt-tour-closed") === "true") {
    return;
  }

  // Load CSS
  function loadCSS(href) {
    var link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = href;
    document.head.appendChild(link);
  }

  // Load JS and call callback when ready
  function loadJS(src, callback) {
    var script = document.createElement("script");
    script.src = src;
    script.onload = callback;
    document.body.appendChild(script);
  }

  loadCSS("/walkthrough/driver.min.css");
  loadCSS("/walkthrough/tour.css");

  loadJS("/walkthrough/driver.min.js", function () {
    loadJS("/walkthrough/tour.js", function () {
      // tour.js self-initializes when loaded
    });
  });
})();
