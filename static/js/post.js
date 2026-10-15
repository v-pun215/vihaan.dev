// Post view: fixed reading progress bar + copy article link.
(function () {
  function onReady(fn) {
    if (document.readyState === "loading") {
      document.addEventListener("DOMContentLoaded", fn, { once: true });
    } else {
      fn();
    }
  }

  onReady(function () {
    var fill = document.getElementById("reading-progress-fill");

    // Reading progress: fill grows with page scroll.
    var ticking = false;
    function updateProgress() {
      ticking = false;
      if (!fill) return;
      var total = document.documentElement.scrollHeight - window.innerHeight;
      var y = window.scrollY || document.documentElement.scrollTop || 0;
      var pct = total > 0 ? Math.min(1, Math.max(0, y / total)) : 0;
      fill.style.width = (pct * 100).toFixed(2) + "%";
    }
    window.addEventListener("scroll", function () {
      if (!ticking) {
        ticking = true;
        window.requestAnimationFrame(updateProgress);
      }
    }, { passive: true });
    window.addEventListener("resize", updateProgress);
    updateProgress();

    // Copy article link.
    var copyBtn = document.getElementById("copy-link");
    var feedback = document.getElementById("copy-feedback");
    if (!copyBtn) return;
    copyBtn.addEventListener("click", function () {
      var url = copyBtn.getAttribute("data-url") || window.location.href;
      copyText(url, function (ok) {
        if (feedback) {
          feedback.textContent = ok ? "Link copied" : "Copy failed";
          setTimeout(function () { feedback.textContent = ""; }, 2000);
        }
        copyBtn.textContent = ok ? "Copied" : "Copy link";
        setTimeout(function () { copyBtn.textContent = "Copy link"; }, 1600);
      });
    });
  });

  function copyText(text, cb) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(function () { cb(true); }, function () { fallbackCopy(text, cb); });
      return;
    }
    fallbackCopy(text, cb);
  }

  function fallbackCopy(text, cb) {
    try {
      var ta = document.createElement("textarea");
      ta.value = text;
      ta.setAttribute("readonly", "");
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      var ok = document.execCommand("copy");
      document.body.removeChild(ta);
      cb(!!ok);
    } catch (e) {
      cb(false);
    }
  }
})();
