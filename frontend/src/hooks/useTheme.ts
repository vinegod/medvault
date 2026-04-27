import { useEffect, useState } from "react";
import { getMeta, setMeta } from "../services/db";

export type Theme = "system" | "light" | "dark";

function applyTheme(theme: Theme) {
  const root = document.documentElement;
  if (theme === "system") {
    const prefersDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    root.setAttribute("data-theme", prefersDark ? "dark" : "light");
  } else {
    root.setAttribute("data-theme", theme);
  }
}

export function useTheme() {
  const [theme, setThemeState] = useState<Theme>("system");

  useEffect(() => {
    getMeta("theme").then((stored) => {
      const t = (stored ?? "system") as Theme;
      setThemeState(t);
      applyTheme(t);
    });
  }, []);

  useEffect(() => {
    if (theme !== "system") return;
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const handler = () => applyTheme("system");
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, [theme]);

  async function setTheme(t: Theme) {
    setThemeState(t);
    applyTheme(t);
    await setMeta("theme", t);
  }

  return { theme, setTheme };
}
