const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");

const html = fs.readFileSync("docs/landing/index.html", "utf8");
const script = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)]
  .map((match) => match[1])
  .findLast((source) => source.includes("data-theme-toggle"));
assert.ok(script, "landing body script exists");

function runTheme(storage, systemDark = true) {
  let click;
  const root = { dataset: {} };
  const toggle = {
    addEventListener(event, callback) {
      if (event === "click") click = callback;
    },
  };
  const document = {
    body: {},
    documentElement: root,
    getElementById() {
      return null;
    },
    querySelector(selector) {
      return selector === "[data-theme-toggle]" ? toggle : null;
    },
    querySelectorAll() {
      return [];
    },
  };
  vm.runInNewContext(script, {
    document,
    localStorage: storage,
    matchMedia: () => ({ matches: systemDark }),
    navigator: {},
    window: { isSecureContext: false },
    Promise,
    setTimeout() {},
    setInterval() {},
  });
  assert.equal(typeof click, "function", "theme click handler is wired");
  click();
  return root.dataset.theme;
}

assert.equal(
  runTheme({
    getItem() {
      throw new Error("storage unavailable");
    },
    setItem() {
      throw new Error("storage unavailable");
    },
  }),
  "light",
  "theme changes when storage reads and writes throw",
);
assert.equal(
  runTheme({
    getItem() {
      return "light";
    },
    setItem() {
      throw new Error("storage unavailable");
    },
  }),
  "dark",
  "saved theme applies and the click still works when writes throw",
);
