"use strict";

// The colour theme: "system" follows the device, "light" and "dark" pin it.
// Loaded in <head> before the stylesheet applies, so pages never flash in the
// wrong theme. The choice is remembered per browser; storage is optional.
(() => {
  const key = "things-theme";
  const modes = ["system", "light", "dark"];
  const root = document.documentElement;

  function get() {
    try {
      const saved = localStorage.getItem(key);
      return modes.includes(saved) ? saved : "system";
    } catch {
      return "system";
    }
  }

  function apply(mode) {
    if (mode === "system") delete root.dataset.theme;
    else root.dataset.theme = mode;
  }

  function set(mode) {
    if (!modes.includes(mode)) return;
    try {
      if (mode === "system") localStorage.removeItem(key);
      else localStorage.setItem(key, mode);
    } catch {
      // Storage is optional; the choice then lasts until the page reloads.
    }
    apply(mode);
    window.dispatchEvent(new CustomEvent("themechange", { detail: mode }));
  }

  apply(get());
  window.appTheme = { modes, set, current: () => root.dataset.theme || "system" };
})();
