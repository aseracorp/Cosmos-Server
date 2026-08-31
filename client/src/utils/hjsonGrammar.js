import Prism from 'prismjs/components/prism-core';

// ==============================|| HJSON GRAMMAR ||============================== //
//
// Cosmos displays compose/JSON payloads as HJSON (see utils/hjson.jsx): keys
// are unquoted, values may be relaxed. Prism's stock `json` grammar only
// recognizes *quoted* keys, so unquoted HJSON keys fall through untokenized
// (rendering white). This grammar extends JSON with HJSON's unquoted keys so
// keys keep their property color (red) and values keep their string color
// (green), exactly like the previous raw-JSON view.
//
// We register it as `hjson`, and override the `json` language with it so the
// compose editor's Prism-based highlighting picks it up transparently.

const hjsonProperty = {
  // Quoted key: identical to JSON's property rule.
  pattern: /(^|[^\\])"(?:\\.|[^\\"\r\n])*"(?=\s*:)/,
  lookbehind: true,
  greedy: true,
};

const hjsonUnquotedProperty = {
  // Unquoted HJSON key: starts after a line start, brace/bracket or comma
  // (plus indentation), extends to the ':' that begins the value. The value
  // is left to the value tokens (string/number/boolean/etc).
  pattern: /(?<=^|[\r\n{[,])\s*[^:#\[\]{}"',\r\n\s][^:#\[\]{}"',\r\n]*(?=\s*:)/,
  greedy: true,
  alias: 'property',
};

const hjsonGrammar = {
  'unquoted-property': hjsonUnquotedProperty,
  property: hjsonProperty,

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
