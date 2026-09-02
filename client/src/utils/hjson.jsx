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
//
// The stored comment text is the FULL, verbatim comment block: original
// leading whitespace, the comment marker (//, # or /* */) and any suffix
// (*/), and every line of a multi-line comment — stored as a single label
// value. Restoration emits those exact lines again, only re-anchoring the
// indentation to the node's current depth (so the comment aligns even after
// the document is re-rendered by toHjson).
// -----------------------------------------------------------------------------

// Extract a map of key-path -> verbatim comment block for every node that has
// a comment immediately above it in the HJSON document. A comment block may be
// one or more consecutive // or # lines, or a /* ... */ block (single or
// multi-line). The whole block is captured as a single label value with its
// original indentation and markers preserved.
export const extractComments = (text) => {
  if (!text || typeof text !== 'string') return {};
  const comments = {};
  const lines = text.split('\n');
  const scope = []; // stack of { indent, key }
  // Pending raw comment lines (verbatim, including indent + markers) before
  // the next key line. Also tracks an open block comment.
  let pending = [];
  let inBlockComment = false;

  const isCommentLine = (line) => {
    const t = line.trim();
    return t.startsWith('//') || t.startsWith('#') || t.startsWith('/*');
  };
  const isBlockEnd = (line) => {
    const t = line.trim();
    return t.indexOf('*/') !== -1;
  };

  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i];
    const trimmed = raw.trim();

    // Collect the raw comment block (verbatim lines).
    if (inBlockComment || (isCommentLine(raw) && !inBlockComment)) {
      pending.push(raw);
      if (!inBlockComment && trimmed.startsWith('/*')) {
        inBlockComment = true;
        if (isBlockEnd(raw)) inBlockComment = false;
      } else if (inBlockComment && isBlockEnd(raw)) {
        inBlockComment = false;
      }
      continue;
    }

    if (!trimmed) { pending = []; inBlockComment = false; continue; }

    const indent = raw.length - raw.trimStart().length;
    while (scope.length && scope[scope.length - 1].indent >= indent) {
      scope.pop();
    }
    if (trimmed === '}' || trimmed === ']' || /^}[,}]?$/.test(trimmed) || /^][,}]?$/.test(trimmed)) {
      pending = [];
      continue;
    }

    const keyMatch = /^("(?:[^"\\]|\\.)*"|[^:#\[\]{}"',\r\n\s/][^:#\[\]{}"',\r\n]*)?\s*:/.exec(trimmed);
    if (keyMatch) {
      let key = keyMatch[1].trim();
      if (key.startsWith('"') && key.endsWith('"')) {
        try { key = JSON.parse(key); } catch (e) { /* keep raw */ }
      }
      const path = scope.map(s => s.key).concat([key]).join('.');
      if (pending.length && !comments[path]) {
        comments[path] = pending.join('\n');
      }
      pending = [];
      const afterColon = trimmed.slice(keyMatch[0].length).trim();
      if (afterColon.startsWith('{') || afterColon.startsWith('[')) {
        scope.push({ indent, key });
      }
      continue;
    }

    pending = [];
  }

  return comments;
};

// Given an HJSON document and a node -> comment map (verbatim comment blocks),
// return a new document with each comment re-inserted on lines immediately
// before its node. Stored indentation is re-anchored to the node's current
// depth; the comment markers and any block-comment suffixes are preserved.
export const injectComments = (text, comments) => {
  if (!comments || !Object.keys(comments).length) return text;
  if (!text || typeof text !== 'string') return text;

  const lines = text.split('\n');
  const scope = [];
  const out = [];
  const inserted = {};

  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i];
    const trimmed = raw.trim();
    const indent = raw.length - raw.trimStart().length;

    while (scope.length && scope[scope.length - 1].indent >= indent) {
      scope.pop();
    }
    if (trimmed === '}' || trimmed === ']' || /^}[,}]?$/.test(trimmed) || /^][,}]?$/.test(trimmed)) {
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
      if (comments[path] && !inserted[path]) {
        inserted[path] = true;
        // The node lives one level deeper than the enclosing scope stack, so
        // its comment aligns with the key line's indentation.
        const nodeIndent = '  '.repeat(scope.length + 1);
        const block = String(comments[path]);
        const blockLines = block.split('\n');
        // Re-anchor the block to the node's indentation, preserving the
        // *relative* indentation of any continuation lines (so multi-line
        // /* */ blocks keep their alignment).
        const baseIndent = (blockLines[0].match(/^\s*/) || [''])[0].length;
        for (const cl of blockLines) {
          const lineIndent = (cl.match(/^\s*/) || [''])[0].length;
          const rel = Math.max(0, lineIndent - baseIndent);
          const content = cl.replace(/^\s+/, '');
          out.push(content ? nodeIndent + ' '.repeat(rel) + content : nodeIndent);
        }
      }
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
