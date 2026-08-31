import Prism from 'prismjs/components/prism-core';

// ==============================|| HJSON GRAMMAR ||============================== //
//
// Cosmos displays compose/JSON payloads as HJSON (see utils/hjson.jsx): keys
// are unquoted and, with quotes: 'min', many string *values* are also
// unquoted. Prism's stock `json` grammar only colors *quoted* keys, so HJSON
// would render mostly white. This grammar extends JSON with HJSON's unquoted
// keys AND unquoted string values, keeping the familiar color scheme:
//   - keys                                    -> property  (red/Okaidia)
//   - string values (quoted or unquoted)      -> string    (green)
//   - numbers / booleans / null               -> number/boolean/null
//   - comments (# //)                          -> comment   (grey)
//
// We register it as `hjson` and override the `json` grammar so the compose
// editor (which resolves the 'json' language) picks it up transparently.

const hjsonProperty = {
  // Quoted key - identical to JSON's property rule.
  pattern: /(^|[^\\])"(?:\\.|[^\\"\r\n])*"(?=\s*:)/,
  lookbehind: true,
  greedy: true,
};

const hjsonUnquotedProperty = {
  // Unquoted HJSON key: starts after a line start, brace/bracket or comma
  // (plus indentation), extends to the ':' that begins the value.
  pattern: /(?<=^|[\r\n{[,])\s*[^:#\[\]{}"',\r\n\s][^:#\[\]{}"',\r\n]*(?=\s*:)/,
  greedy: true,
  alias: 'property',
};

const hjsonUnquotedValue = {
  // Unquoted string value right after 'key: ' (HJSON 'min' emits one space).
  // Negative lookahead keeps numbers / booleans / null their own colors.
  pattern: /(?<=:\s)(?!true\b|false\b|null\b|-?\b\d[\d.e+-]*\b)[^#"',{}\[\]\r\n]+/,
  greedy: true,
  alias: 'string',
};

const hjsonArrayElement = {
  // Unquoted string element inside [ ... ] (own line after [, or ,).
  // The (?!\s) guard stops whitespace-only (closing-bracket) lines from
  // being painted as string.
  pattern: /(?<=[\r\n,\[])\s*(?!\s|true\b|false\b|null\b|-?\b\d[\d.e+-]*\b)[^#"',{}\[\]\r\n]+/,
  greedy: true,
  alias: 'string',
};

const hjsonGrammar = {
  'unquoted-property': hjsonUnquotedProperty,
  property: hjsonProperty,
  'unquoted-string': hjsonUnquotedValue,
  'array-string': hjsonArrayElement,

  string: {
    pattern: /(^|[^\\])"(?:\\.|[^\\"\r\n])*"(?!\s*:)/,
    lookbehind: true,
    greedy: true,
  },

  comment: {
    pattern: /(?:\/\/.*|\/\*[\s\S]*?(?:\*\/|$)|#.*$)/m,
    greedy: true,
  },

  number: /-?\b\d+(?:\.\d+)?(?:e[+-]?\d+)?\b/i,

  punctuation: /[{}[\],]/,

  operator: /:/,

  boolean: /\b(?:false|true)\b/,

  null: {
    pattern: /\bnull\b/,
    alias: 'keyword',
  },
};

Prism.languages.hjson = hjsonGrammar;

// Override the stock json language so existing `.json` highlight callers
// (e.g. the compose editor) also render HJSON colors.
Prism.languages.json = hjsonGrammar;

// Register hjson as an alias of json for any contextual language (like
// react-simple-code-editor resolves languages by name). Safe to call
// multiple times.
export const registerHjson = (name = 'json') => {
  Prism.languages[name] = hjsonGrammar;
};

export default hjsonGrammar;
