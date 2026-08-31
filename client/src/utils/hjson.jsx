import Hjson from 'hjson';

// ==============================|| HJSON HELPERS ||============================== //
//
// Cosmos stores service/compose payloads as JSON. For display and editing we
// prefer HJSON (https://hjson.github.io/) — a human-friendly superset of JSON
// that allows comments, unquoted keys and relaxed strings. Since HJSON is a
// superset of JSON, every valid JSON document is already valid HJSON, so the
// backend never notices the difference: we only convert at the edges of the UI.
//
// NOTE on quotes: we always quote string values ("value") and add trailing
// separators, so the output is unambiguous and reads like JSON with unquoted
// keys. Strings containing #, //, : or escaped quotes stay safely inside
// quotes and are never misread as comments or structure.

// Render a JS object as pretty HJSON.
export const toHjson = (obj) => {
  try {
    return Hjson.stringify(obj, {
      space: 2,
      quotes: 'always',
      separator: true,
      bracesSameLine: true,
    });
  } catch (e) {
    // Never break the UI on a malformed payload — fall back to JSON.
    return JSON.stringify(obj, null, 2);
  }
};

// Parse text that may be either JSON or HJSON (HJSON accepts JSON, but being
// explicit costs nothing and keeps intent clear). Returns the parsed object,
// or throws with a normalized, user-facing message on invalid input.
export const parseJsonOrHjson = (text) => {
  const trimmed = text == null ? '' : String(text).trim();
  if (!trimmed) {
    throw new Error('Empty input');
  }

  // Fast path: plain JSON is the common case for machine-written payloads.
  let jsonErr;
  try {
    return JSON.parse(trimmed);
  } catch (err) {
    jsonErr = err;
    // Fall through to HJSON, which tolerates comments / unquoted keys.
  }

  try {
    return Hjson.parse(trimmed);
  } catch (hjsonErr) {
    // Prefer the JSON error message when the document actually looked like
    // JSON; otherwise surface the HJSON one.
    const looksLikeJson = /^[\s]*[[{]/.test(trimmed);
    const err = looksLikeJson && jsonErr ? jsonErr : hjsonErr;
    throw new Error(err && err.message ? err.message : 'Invalid JSON/HJSON input');
  }
};

// True when the text contains HJSON-specific syntax that would be lost in a
// plain JSON round-trip: // # or /* */ comments, unquoted keys (a bare
// identifier before ':'), or single-quoted strings. Plain JSON documents
// (machine-generated, no comments) return false so callers can skip the
// "$$raw" payload entirely and keep the request byte-identical to a JSON one.
export const hasHjsonSyntax = (text) => {
  const trimmed = text == null ? '' : String(text);
  if (!trimmed) return false;
  // Quick reject: if JSON.parse succeeds there can be no comments/unquoted
  // keys, so it is plain JSON regardless of single quotes (JSON forbids
  // those, so a success means no HJSON-only tokens outside strings).
  try {
    JSON.parse(trimmed);
    return false;
  } catch (e) {
    // fall through
  }
  // HJSON-only markers: comments, unquoted keys, single-quoted strings.
  // ':' followed by a value on unquoted keys is the strongest signal, but
  // comments are the common one users type. Use a conservative check so we
  // only skip $$raw when definitely plain JSON.
  return (
    /\/\/|#|(?:\/[*])|(?:\r?\n\s*[A-Za-z_$][\w$.-]*(?=\s*:))/.test(trimmed)
  );
};