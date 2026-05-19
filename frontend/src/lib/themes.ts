// Catalog of the 6 themes the user can pick from in Settings → Appearance.
//
// Each theme is *dark* — the app has no light mode and supporting one would
// be a much larger change than just adding a stylesheet (every component is
// hand-tuned for dark surfaces). What varies between themes is the *accent*:
// the colour used for primary buttons, focus rings, selection highlights,
// the active-nav indicator, and the download series in the speed graph.
//
// Status colours (seed/paused/fail) intentionally stay constant across
// themes because they carry semantic meaning — green/amber/red — and
// re-skinning them per theme would degrade scannability.
//
// The actual colour ramps live in index.css inside `html[data-theme="<id>"]`
// blocks; this file is just the metadata used by the picker UI.

export type ThemeID = 'amethyst' | 'cobalt' | 'verdant' | 'ember' | 'rose' | 'mono';

export type Theme = {
  id: ThemeID;
  label: string;
  // CSS colour string used as the swatch dot in the picker. Matches the
  // theme's accent-500 value in index.css.
  swatch: string;
  blurb: string;
};

export const THEMES: Theme[] = [
  {id: 'amethyst', label: 'Amethyst', swatch: 'oklch(0.65 0.25 290)', blurb: 'Signature purple — the default.'},
  {id: 'cobalt',   label: 'Cobalt',   swatch: 'oklch(0.65 0.25 250)', blurb: 'Deep, classic blue.'},
  {id: 'verdant',  label: 'Verdant',  swatch: 'oklch(0.70 0.20 155)', blurb: 'Emerald green.'},
  {id: 'ember',    label: 'Ember',    swatch: 'oklch(0.72 0.18 45)',  blurb: 'Warm, glowing orange.'},
  {id: 'rose',     label: 'Rose',     swatch: 'oklch(0.68 0.22 0)',   blurb: 'Pink-magenta.'},
  {id: 'mono',     label: 'Mono',     swatch: 'oklch(0.78 0.05 240)', blurb: 'Desaturated cool slate.'},
];

export const DEFAULT_THEME: ThemeID = 'amethyst';

const VALID_IDS = new Set<string>(THEMES.map((t) => t.id));

export function isThemeID(v: unknown): v is ThemeID {
  return typeof v === 'string' && VALID_IDS.has(v);
}
