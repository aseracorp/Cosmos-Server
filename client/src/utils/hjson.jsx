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
// -----------------------------------------------------------------------------
// Comment preservation (cosmos.compose.<node> labels)
//
// HJSON comments are not representable in the JSON payload the backend
// stores, so we extract a node -> comment map client-side before saving and
// restore it into the editor on load. Keys mirror the JSON key path
// (e.g. "services.web.image"), matching the cosmos.compose.<path> labels the
// backend persists.
// -----------------------------------------------------------------------------

// Extract a map of key-path -> comment text for every node that has a comment
// immediately above it in the HJSON document. Handles #, // and /* */ style
// comments. Uses indentation (the editor emits 2-space indent, braces on the
// same line) to track the current key path, which is robust for the documents
// the compose editor produces/edits.
export const extractComments = (text) => {
  if (!text || typeof text !== 'string') return {};
  const comments = {};
  const lines = text.split('\n');
  // Stack of { indent, key } for the enclosing objects/arrays.
  const scope = []; // each: { indent, key }
  // Pending comment lines accumulated before the next key line.
  let pending = [];

  const stripComment = (line) => {
    const t = line.trim();
    if (t.startsWith('//')) return t.replace(/^\s*\/\/\s*/, '').trim();
    if (t.startsWith('#')) return t.replace(/^\s*#\s*/, '').trim();
    if (t.startsWith('/*')) return t.replace(/^\s*\/\*\s*/, '').replace(/\s*\*\/\s*$/, '').trim();
    return null;
  };

  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i];
    const trimmed = raw.trim();
    if (!trimmed) { pending = []; continue; } // blank resets pending

    const c = stripComment(raw);
    if (c !== null) { pending.push(c); continue; }

    const indent = raw.length - raw.trimStart().length;
    const indentMatch = /^(\s*)?/.exec(raw)[0].length;

    // Pop scopes deeper than current indent (dedent).
    while (scope.length && scope[scope.length - 1].indent >= indent) {
      scope.pop();
    }

    // Closing brace/bracket: pop scope and reset pending.
    if (trimmed === '}' || trimmed === ']' || /^}[,}]?$/.test(trimmed) || /^][,}]?$/.test(trimmed)) {
      if (scope.length) scope.pop();
      pending = [];
      continue;
    }

    // Parse "key: value" — key can be quoted or unquoted.
    const keyMatch = /^("(?:[^"\\]|\\.)*"|[^:#\[\]{}"',\r\n\s/][^:#\[\]{}"',\r\n]*)?\s*:/.exec(trimmed);
    if (keyMatch) {
      let key = keyMatch[1].trim();
      if (key.startsWith('"') && key.endsWith('"')) {
        try { key = JSON.parse(key); } catch (e) { /* keep raw */ }
      }
      // Build the full path from the current scope + this key.
      const pathParts = scope.map(s => s.key).concat([key]);
      const path = pathParts.join('.');
      if (pending.length) {
        if (!comments[path]) {
          comments[path] = pending.join('\n');
        }
        pending = [];
      }
      // If value opens an object/array, push this key onto the scope.
      const afterColon = trimmed.slice(keyMatch[0].length).trim();
      if (afterColon === '{' || afterColon === '[' || afterColon.startsWith('{') || afterColon.startsWith('[')) {
        scope.push({ indent, key });
      } else {
        // value is scalar or inline object; no scope push
        if (afterColon.startsWith('{') || afterColon.startsWith('[')) {
          scope.push({ indent, key });
        }
      }
      continue;
    }

    // Array element line (e.g. "- item" or scalar inside a [...]) — treated
    // as a child of the last array scope; not comment-targeted by key.
    pending = [];
  }

  return comments;
};

// Given an HJSON document and a node -> comment map, return a new document
// with each comment inserted on a line immediately before its node.
export const injectComments = (text, comments) => {
  if (!comments || !Object.keys(comments).length) return text;
  if (!text || typeof text !== 'string') return text;

  const lines = text.split('\n');
  const scope = [];
  const out = [];
  const inserted = {}; // path already inserted (avoid duplicates)

  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i];
    const trimmed = raw.trim();
    const indent = raw.length - raw.trimStart().length;

    while (scope.length && scope[scope.length - 1].indent >= indent) {
      scope.pop();
    }
    if (trimmed === '}' || trimmed === ']' || /^}[,}]?$/.test(trimmed) || /^][,}]?$/.test(trimmed)) {
      if (scope.length) scope.pop();
      out.push(raw);
      continue;
    }

    const keyMatch = /^("(?:[^"\\]|\\.)*"|[^:#\[\]{}"',\r\n\s/][^:#\[\]{}"',\r\n]*)?\s*:/.exec(trimmed);
    if (keyMatch) {
      let key = keyMatch[1].trim();
      if (key.startsWith('"') && key.endsWith('"')) {
        try { key = JSON.parse(key); } catch (e) { /* keep */ }
      }
      const path = scope.map(s => s.key).concat([key]).join('.');
      let hasComment = false;
      if (comments[path] && !inserted[path]) {
        inserted[path] = true;
        hasComment = true;
        const indentStr = '  '.repeat(scope.length);
        const commentText = String(comments[path]);
        for (const cl of commentText.split('\n')) {
          out.push(indentStr + '// ' + cl);
        }
      }
      // Always emit the original key line after any inserted comments.
      out.push(raw);
      const afterColon = trimmed.slice(keyMatch[0].length).trim();
      if (afterColon.startsWith('{') || afterColon.startsWith('[')) {
        scope.push({ indent, key });
      }
      continue;
    }
    out.push(raw);
  }

  return out.join('\n');
};
