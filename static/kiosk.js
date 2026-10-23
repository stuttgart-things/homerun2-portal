// homerun2 kiosk (/kiosk): shows the slides the page lists one after another.
//
// - A service that is down (/api/services) is skipped; the overview never is.
// - At most two frames exist: the one shown and the next one loading. The
//   switch happens once the next has loaded, so there is no blank page, and
//   the event streams of hidden service UIs do not pile up.
// - Keyboard, mouse and touch input, also inside the frame (same origin),
//   hold the current slide for data-pause seconds; data-reset seconds after
//   the last input the kiosk reloads and starts over.
(function () {
    "use strict";

    var root = document.getElementById("hr-kiosk");
    var stage = document.getElementById("hr-kiosk-stage");
    var titleEl = document.getElementById("hr-kiosk-title");
    var countEl = document.getElementById("hr-kiosk-count");
    var bar = document.getElementById("hr-kiosk-bar");

    var seconds = Number(root.dataset.seconds) * 1000;
    var pause = Number(root.dataset.pause) * 1000;
    var reset = Number(root.dataset.reset) * 1000;
    var refresh = Math.max(1, Number(root.dataset.refresh)) * 1000;
    var loadTimeout = 15000;

    var slides = Array.prototype.map.call(root.querySelectorAll(".hr-kiosk-slides li"), function (li) {
        return { key: li.dataset.key, title: li.dataset.title, url: li.dataset.url, zoom: Number(li.dataset.zoom) || 100 };
    });
    if (slides.length === 0) return;

    var down = {};          // key → true while the service is down
    var current = -1;       // index of the slide shown
    var shown = null;       // its frame
    var loading = null;     // the next frame, until it has loaded
    var shownAt = 0;        // when the current slide appeared
    var lastInput = 0;      // last keyboard/mouse/touch input, 0 = none yet

    function isUp(i) { return slides[i].key === "overview" || !down[slides[i].key]; }

    // The next slide after the current one that is up; the current one if
    // no other is (or none is: then it stays rather than going blank).
    function nextIndex() {
        for (var k = 1; k <= slides.length; k++) {
            var j = (current + k) % slides.length;
            if (isUp(j)) return j;
        }
        return current < 0 ? 0 : current;
    }

    function onInput() { lastInput = Date.now(); }

    function watchInput(win) {
        ["keydown", "pointerdown", "pointermove", "wheel", "touchstart"].forEach(function (type) {
            win.addEventListener(type, onInput, { passive: true, capture: true });
        });
    }

    function zoomFrame(frame, zoom) {
        if (zoom === 100) return;
        var f = zoom / 100;
        frame.style.width = (100 / f) + "%";
        frame.style.height = (100 / f) + "%";
        frame.style.transform = "scale(" + f + ")";
    }

    function show(i) {
        if (loading) return;
        var s = slides[i];
        var frame = document.createElement("iframe");
        frame.className = "hr-kiosk-frame";
        frame.title = s.title;
        zoomFrame(frame, s.zoom);
        var done = false;
        var reveal = function () {
            if (done) return;
            done = true;
            frame.classList.add("on");
            if (shown) stage.removeChild(shown);
            shown = frame;
            loading = null;
            current = i;
            shownAt = Date.now();
            titleEl.textContent = s.title;
            countEl.textContent = slides.length > 1 ? (i + 1) + " / " + slides.length : "";
        };
        // Every page loaded into the frame, also by a click inside it, is a
        // new window to watch.
        frame.addEventListener("load", function () {
            try { watchInput(frame.contentWindow); } catch (e) { /* not same origin */ }
            reveal();
        });
        setTimeout(reveal, loadTimeout);
        frame.src = s.url;
        loading = frame;
        stage.appendChild(frame);
    }

    function tick() {
        var now = Date.now();
        if (reset > 0 && lastInput > 0 && now - lastInput > reset) {
            location.reload();
            return;
        }
        if (current < 0 || seconds === 0 || slides.length < 2) {
            bar.style.width = "0";
            return;
        }
        // Input holds the slide: it is due a full pause after the last input.
        var due = Math.max(shownAt + seconds, pause > 0 && lastInput > 0 ? lastInput + pause : 0);
        root.classList.toggle("paused", due > shownAt + seconds);
        bar.style.width = Math.min(100, 100 * (now - shownAt) / (due - shownAt)) + "%";
        var next = nextIndex();
        if ((now >= due || !isUp(current)) && next !== current) show(next);
    }

    function readStatus() {
        fetch("/api/services", { cache: "no-store" })
            .then(function (r) { return r.ok ? r.json() : []; })
            .then(function (list) {
                down = {};
                list.forEach(function (st) {
                    if (st.checked && !st.up) down[st.key] = true;
                    slides.forEach(function (s, i) {
                        if (s.key === st.key && st.title) {
                            s.title = st.title;
                            if (i === current) titleEl.textContent = st.title;
                        }
                    });
                });
            })
            .catch(function () { /* keep the last status */ });
    }

    watchInput(window);
    readStatus();
    setInterval(readStatus, refresh);
    show(nextIndex());
    setInterval(tick, 250);
})();
