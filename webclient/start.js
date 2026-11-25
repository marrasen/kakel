// What the page runs first: it loads the client, shows how far that has
// come, and says in words when it cannot.
//
// It is written for browsers too old for the client, so they can be told.
// What follows the # in the page's address is the guest's secret, and
// nothing here reads it: the client does, and takes it off the address
// once it is of no more use. See dropSecret in browser.go.
(function () {
  "use strict";

  var body = document.body;
  var title = document.getElementById("title");
  var text = document.getElementById("text");
  var bar = document.getElementById("bar");
  var icon = document.getElementById("icon");
  var fill = document.getElementById("fill");
  var retry = document.getElementById("retry");

  // How large the client is unpacked, which is what a browser counts in,
  // and 0 when the page does not know.
  var size = Number(document.querySelector('meta[name="kakel-size"]').content) || 0;
  // How long a download may stand still, and the client may be waited
  // for when the relay is busy.
  var stillFor = 60000, waits = 10;

  // What is said. retry is what the button does, when there is one.
  var old = { title: "This browser is too old", text: "Update it, or open the link in a current Chrome, Edge, Firefox or Safari." };
  var cannot = { title: "This browser can't run kakel", text: "Open the link in a current Chrome, Edge, Firefox or Safari." };
  var graphics = { title: "WebGL 2 is off", text: "Turn on graphics acceleration in the browser's settings, or open the link in another browser.", retry: reload };
  var lost = { title: "Download failed", text: "Check the network, then retry.", retry: load };
  var other = { title: "Download failed", text: "It arrived incomplete.", retry: reload };
  var memory = { title: "Not enough memory", text: "Close other tabs, then retry.", retry: reload };
  var stopped = { title: "kakel stopped", text: "An error closed it.", retry: reload };

  var failed = null, turning = 0, asked = 0, breathing = 0;

  function reload() { location.reload(); }

  // problem is why there is no client: what is said, and what is logged.
  function problem(says, why) { return { says: says, why: why }; }

  // problemOf is the problem err is, or for any other error says.
  function problemOf(err, says) { return err && err.says ? err : problem(says, messageOf(err)); }

  // messageOf is what an error says, and "" for none.
  function messageOf(err) { return err ? String(err.message || err) : ""; }

  // turn fades what is said over to what write writes.
  function turn(write) {
    clearTimeout(turning);
    body.classList.add("turning");
    turning = setTimeout(function () {
      write();
      body.classList.remove("turning");
    }, 160);
  }

  // fail says why there is no client, for good or until the button is
  // clicked, and logs it.
  function fail(p) {
    if (failed) return;
    var says = failed = p.says;
    console.error("kakel: " + says.title + (p.why ? ": " + p.why : ""));
    function write() {
      body.classList.remove("waiting", "busy");
      body.classList.add("failed");
      icon.classList.remove("out");
      body.classList.toggle("again", !!says.retry);
      title.textContent = says.title;
      text.textContent = says.text;
      if (says.retry) retry.focus();
    }
    if (body.classList.contains("ready")) {
      // The page fades in over the window with it said already.
      body.classList.remove("ready");
      write();
    } else {
      turn(write);
    }
  }

  // loading says the client is on its way.
  function loading() {
    body.classList.remove("failed", "again");
    body.classList.add("waiting");
    title.textContent = "Loading";
    text.textContent = "";
    progress(0);
    if (!breathing) {
      breathing = setInterval(breathe, 2400);
      breathe();
    }
  }

  // breathe has the icon grow, or go back, while the page waits.
  function breathe() {
    if (body.classList.contains("waiting") && !body.classList.contains("ready")) {
      icon.classList.toggle("out");
      return;
    }
    clearInterval(breathing);
    breathing = 0;
    icon.classList.remove("out");
  }

  // progress shows how much of the client has come, of 1. Where that
  // cannot be told, and while what came is made ready, the bar is busy.
  function progress(part) {
    var known = size > 0 && part < 1;
    body.classList.toggle("busy", !known);
    if (known) {
      fill.style.transform = "scaleX(" + part + ")";
      bar.setAttribute("aria-valuenow", String(Math.round(part * 100)));
    } else {
      // All of it, which the page's style says: one here would go first.
      fill.style.transform = "";
      bar.removeAttribute("aria-valuenow");
    }
  }

  retry.addEventListener("click", function () {
    var again = failed && failed.retry;
    if (!again) return;
    failed = null;
    if (again === reload) { reload(); return; }
    turn(function () {
      loading();
      again();
    });
  });

  // script loads the script that runs Go's programs.
  function script() {
    return new Promise(function (done, not) {
      if (window.Go) { done(); return; }
      var s = document.createElement("script");
      s.src = "wasm_exec.js";
      s.onload = function () { if (window.Go) done(); else not(problem(old, "wasm_exec.js did not run")); };
      s.onerror = function () { s.remove(); not(problem(lost, "wasm_exec.js did not arrive")); };
      document.head.appendChild(s);
    });
  }

  // program downloads and compiles the client.
  function program() {
    var quit = window.AbortController ? new AbortController() : null;
    var timer = 0, broke = null, got = 0;
    function moved() {
      clearTimeout(timer);
      timer = setTimeout(function () { if (quit) quit.abort(); }, stillFor);
    }
    moved();
    return fetch("client.wasm", quit ? { signal: quit.signal } : {}).then(function (r) {
      if (r.status === 503 && asked < waits) {
        // The relay has only just started, or is sending it to many.
        asked++;
        drop(r);
        var after = Math.min(Number(r.headers.get("Retry-After")) || 3, 10);
        return new Promise(function (done) { setTimeout(done, after * 1000); }).then(function () { clearTimeout(timer); return program(); });
      }
      if (!r.ok) return refused(r);
      var from = r.body.getReader();
      // The same bytes, counted on their way to the compiler.
      var counted = new ReadableStream({
        pull: function (to) {
          return from.read().then(function (part) {
            if (part.done) {
              clearTimeout(timer);
              if (size > 0 && got !== size) {
                broke = other;
                throw new Error(got + " bytes of " + size);
              }
              progress(1);
              to.close();
              return;
            }
            moved();
            got += part.value.length;
            progress(Math.min(got / size, .99));
            to.enqueue(part.value);
          }).catch(function (err) {
            broke = broke || lost;
            to.error(err);
          });
        },
        cancel: function (why) { return from.cancel(why); }
      });
      var wasm = new Response(counted, { headers: { "Content-Type": "application/wasm" } });
      return WebAssembly.compileStreaming ? WebAssembly.compileStreaming(wasm)
        : wasm.arrayBuffer().then(function (b) { return WebAssembly.compile(b); });
    }).catch(function (err) {
      clearTimeout(timer);
      throw problemOf(err, broke || (err instanceof WebAssembly.CompileError ? old : tooLarge(err) ? memory : lost));
    });
  }

  // drop lets go of an answer nothing more is read of.
  function drop(r) { if (r.body) r.body.cancel().catch(function () {}); }

  // refused is what becomes of an answer that is not the client: the page
  // says what the relay does, in the relay's own words. They are its own
  // page's heading and paragraph, as newNotice in relay/guestdoor.go
  // writes it. Any other answer is a download that failed.
  function refused(r) {
    var why = "the relay answered " + r.status;
    if (r.status !== 404 && r.status !== 429) {
      drop(r);
      throw problem(lost, why);
    }
    return r.text().then(function (page) {
      var doc = new DOMParser().parseFromString(page, "text/html");
      var head = doc.querySelector("main > h1"), under = doc.querySelector("main > p");
      if (!head || !under) throw problem(lost, why);
      // A link that has ended stays so, and one tried too often does not.
      throw problem({ title: head.textContent, text: under.textContent, retry: r.status === 429 ? load : null }, why);
    });
  }

  // tooLarge reports whether an error is of memory there is none of.
  function tooLarge(err) { return err instanceof RangeError || /memory/i.test(messageOf(err)); }

  // run runs the client, and returns once it has ended, which it only
  // does when something went wrong.
  function run(module) {
    var go = new window.Go();
    var code = 0;
    go.exit = function (c) { code = c; };
    return WebAssembly.instantiate(module, go.importObject).then(function (instance) {
      // It is not done when this has run: it waits for what comes.
      window.addEventListener("error", function (e) { if (crashed(e.error)) e.preventDefault(); });
      window.addEventListener("unhandledrejection", function (e) { if (crashed(e.reason)) e.preventDefault(); });
      return go.run(instance);
    }, function (err) {
      throw problemOf(err, tooLarge(err) ? memory : old);
    }).then(function () {
      throw problem(stopped, "exit code " + code);
    });
  }

  // crashed takes an error the client met while it ran, when it is one
  // it cannot go on after, or one that comes of having stopped.
  function crashed(err) {
    if (failed === stopped) return true;
    if (!(err instanceof WebAssembly.RuntimeError || err instanceof RangeError)) return false;
    fail(problemOf(err, stopped));
    return true;
  }

  // load loads the client and runs it.
  function load() {
    asked = 0;
    script().then(program).then(run).catch(function (err) { fail(problemOf(err, stopped)); });
  }

  loading();
  if (typeof WebAssembly !== "object" || !window.fetch || !window.Promise || !window.ReadableStream || !window.TextEncoder) {
    fail(problem(cannot, "no WebAssembly"));
    return;
  }
  var gl = document.createElement("canvas").getContext("webgl2");
  if (!gl) {
    fail(problem(graphics, ""));
    return;
  }
  // Given back: a page may have only so many.
  var ext = gl.getExtension("WEBGL_lose_context");
  if (ext) ext.loseContext();
  load();
})();
