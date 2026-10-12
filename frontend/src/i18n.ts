// Magpie-style explicit dictionaries and {name} interpolation. Only authored
// UI text passed to t() is localized; skills, paths and repository content are
// always displayed as received.
import { en } from "./locales/en";
import { zhCN } from "./locales/zh-CN";
import { zhTW } from "./locales/zh-TW";
import { api } from "./api";
import { backendMessages } from "./locales/backend";

export type Locale = "zh-CN" | "zh-TW" | "en";
export type LocalePreference = Locale | "system";
export type TranslationKey = keyof typeof en;
export type TranslationVariables = Record<string, string | number>;
export const supportedLocales: readonly { id: Locale; label: string }[] = [
  { id: "zh-CN", label: "简体中文" },
  { id: "zh-TW", label: "繁體中文" },
  { id: "en", label: "English" },
];
const storageKey = "racomi.locale";
const dictionaries: Record<Locale, Readonly<Record<string, string>>> = { en, "zh-CN": zhCN, "zh-TW": zhTW };

export function systemLocale(language = navigator.languages?.[0] || navigator.language || "en"): Locale {
  if (/^zh-(?:tw|hk|mo|hant)(?:-|$)/i.test(language)) return "zh-TW";
  return /^zh(?:-|$)/i.test(language) ? "zh-CN" : "en";
}

function readPreference(): LocalePreference {
  try {
    const stored = localStorage.getItem(storageKey);
    if (stored === "en" || stored === "zh-CN" || stored === "zh-TW") return stored;
  } catch { /* A language choice also works without browser storage. */ }
  return "system";
}

let preference = readPreference();
let locale: Locale = preference === "system" ? systemLocale() : preference;
let nativeReady = false;

function syncNativeLocale() {
  if (nativeReady) void api.setLocale(locale).catch(() => { /* Browser mode keeps its own locale. */ });
}

function applyLocale() {
  const next = preference === "system" ? systemLocale() : preference;
  const changed = locale !== next || document.documentElement.dataset.localePreference !== preference;
  locale = next;
  document.documentElement.lang = locale;
  document.documentElement.dataset.localePreference = preference;
  syncNativeLocale();
  if (changed) document.dispatchEvent(new CustomEvent("racomi:locale-change", { detail: { preference, locale } }));
}

export function getLocale(): Locale { return locale; }
export function getLocalePreference(): LocalePreference { return preference; }
export function enableNativeLocaleSync() { nativeReady = true; syncNativeLocale(); }
export function setLocale(next: LocalePreference) {
  if (next !== "system" && !supportedLocales.some((item) => item.id === next)) return;
  preference = next;
  try { localStorage.setItem(storageKey, next); } catch { /* Keep this session's choice. */ }
  applyLocale();
}

export function t(key: string, variables?: TranslationVariables): string {
  const translated = Object.hasOwn(dictionaries[locale], key) ? dictionaries[locale][key] : Object.hasOwn(dictionaries.en, key) ? dictionaries.en[key] : key;
  return variables ? translated.replace(/\{(\w+)\}/g, (slot, name: string) => Object.hasOwn(variables, name) ? String(variables[name]) : slot) : translated;
}

const backendExact = new Map(Object.entries(backendMessages).flatMap(([key, translations]) => [...new Set([key, ...Object.values(translations)])].filter((value) => !/\{\w+\}/.test(value)).map((value) => [value, translations] as const)));

// Only these diagnostic wrappers contain another backend message in slot 1.
const backendReasonTemplates = new Set([
  "unsupported skill content at {0}: {1}",
  "skipped repository entry {0}: {1}",
  "skill {0} was skipped during refresh: {1}",
  "no usable skills were found; skipped {0}: {1}",
]);

const backendTemplates = Object.entries(backendMessages).flatMap(([key, translations]) => [...new Set([key, ...Object.values(translations)])].filter((value) => /\{\w+\}/.test(value)).map((template) => {
  const reasonSlot = backendReasonTemplates.has(key) ? "1" : undefined;
  const names: string[] = [];
  const parts = template.split(/\{(\w+)\}/g);
  const pattern = parts.map((part, index) => {
    if (index % 2) {
      names.push(part);
      // Go quotes paths and skill names; delimiters inside them are user data.
      return reasonSlot && part === "0" ? String.raw`("(?:\\.|[^"\\])*"|[\s\S]*?)` : "([\\s\\S]*?)";
    }
    return part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  }).join("");
  return { translations, names, reasonSlot, pattern: new RegExp(`^${pattern}$`), specificity: parts.filter((_, index) => index % 2 === 0).join("").length };
})).sort((a, b) => b.specificity - a.specificity);

// Translate known messages and explicitly marked nested diagnostic reasons.
// Captured names, paths, commands and unknown reasons remain verbatim.
export function tMessage(message: string | null | undefined): string {
  if (!message) return "";
  const exact = Object.hasOwn(backendMessages, message) ? backendMessages[message] : backendExact.get(message);
  if (exact) return exact[locale];
  for (const entry of backendTemplates) {
    const match = message.match(entry.pattern);
    if (!match) continue;
    const variables = Object.fromEntries(entry.names.map((name, index) => [name, match[index + 1]]));
    if (entry.reasonSlot) variables[entry.reasonSlot] = tMessage(variables[entry.reasonSlot]);
    return entry.translations[locale].replace(/\{(\w+)\}/g, (slot, name: string) => Object.hasOwn(variables, name) ? variables[name] : slot);
  }
  return message;
}

applyLocale();
window.addEventListener("languagechange", () => { if (preference === "system") applyLocale(); });
window.addEventListener("storage", (event) => {
  if (event.key === storageKey || event.key === null) { preference = readPreference(); applyLocale(); }
});
