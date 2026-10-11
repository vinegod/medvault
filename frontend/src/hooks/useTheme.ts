import { useEffect, useState } from "react";
import { getMeta, setMeta } from "../services/db";

export type Theme = "system" | "light" | "dark";

let current: Theme = "system";

function applyTheme(theme: Theme) {
  const root = document.documentElement;
  if (theme === "system") {
    const prefersDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    root.setAttribute("data-theme", prefersDark ? "dark" : "light");
  } else {
    root.setAttribute("data-theme", theme);
  }
}

// initTheme applies the stored theme at startup and follows OS changes
// while the theme is "system".
export async function initTheme() {
  applyTheme(current);
  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
    if (current === "system") applyTheme("system");
  });
  current = ((await getMeta("theme")) ?? "system") as Theme;
  applyTheme(current);
}

export function useTheme() {
  const [theme, setThemeState] = useState<Theme>(current);

  useEffect(() => {
    getMeta("theme").then((stored) => setThemeState((stored ?? "system") as Theme));
  }, []);

  async function setTheme(t: Theme) {
    current = t;
    setThemeState(t);
    applyTheme(t);
    await setMeta("theme", t);
  }

  return { theme, setTheme };
}
