// Theme state: dark by default, light via toggle. Persisted in localStorage
// and applied as a class on <html> so Tailwind's `dark:` variant (configured
// against `.dark` in styles.css) works everywhere.

import { createContext, createElement, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { STORAGE_KEYS } from "../api/client";

export type Theme = "dark" | "light";

function readStoredTheme(): Theme {
  const stored = localStorage.getItem(STORAGE_KEYS.theme);
  return stored === "light" ? "light" : "dark";
}

function applyTheme(theme: Theme) {
  const root = document.documentElement;
  root.classList.toggle("dark", theme === "dark");
  root.classList.toggle("light", theme === "light");
}

interface ThemeState {
  theme: Theme;
  toggleTheme: () => void;
}

const ThemeContext = createContext<ThemeState | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(readStoredTheme);

  useEffect(() => {
    applyTheme(theme);
    localStorage.setItem(STORAGE_KEYS.theme, theme);
  }, [theme]);

  const toggleTheme = useCallback(() => {
    setTheme((t) => (t === "dark" ? "light" : "dark"));
  }, []);

  return createElement(ThemeContext.Provider, { value: { theme, toggleTheme } }, children);
}

export function useTheme(): ThemeState {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error("useTheme must be used within a ThemeProvider");
  return ctx;
}
