// Byte-size helpers shared by the servapp editors.

// Binary multipliers, as docker uses them for RAM limits.
const UNITS = {
  k: 1024,
  m: 1024 * 1024,
  g: 1024 * 1024 * 1024,
  t: 1024 * 1024 * 1024 * 1024,
  p: 1024 * 1024 * 1024 * 1024 * 1024,
};

// parseBytes turns a docker-style byte size ("512m", "1g", "512mb", "1073741824")
// into a byte count. It mirrors docker's RAMInBytes: units are binary,
// case-insensitive, the "b"/"ib" tail is optional, and a bare number is bytes.
//
// Returns 0 for empty input (unlimited). Returns -1 when the value cannot be
// parsed, so callers can tell "unset" from "typo" instead of silently saving 0.
export const parseBytes = (raw) => {
  if (raw == null) return 0;
  const s = String(raw).trim();
  if (s === "") return 0;

  // Bare number (raw bytes) — this is what a round-trip through
  // containerInfo.HostConfig.Memory looks like.
  if (/^\d+(\.\d+)?$/.test(s)) return Math.round(parseFloat(s));

  // Number + optional suffix. "-1" is a valid docker value for swap ("unlimited").
  const m = s.match(/^(-?\d+(?:\.\d+)?)\s*([a-zA-Z]+)$/);
  if (!m) {
    if (/^-?\d+(\.\d+)?$/.test(s)) return Math.round(parseFloat(s));
    return -1;
  }
  const num = parseFloat(m[1]);
  const suffix = m[2].toLowerCase();
  if (suffix === "b") return Math.round(num);
  const mul = UNITS[suffix[0]];
  if (!mul) return -1;
  // Suffix must be "m", "mb", "mib" — reject typos like "mxx".
  if (suffix.length === 2 && suffix[1] !== "b") return -1;
  if (suffix.length === 3 && suffix.slice(1) !== "ib") return -1;
  return Math.round(num * mul);
};

// formatBytes renders a byte count back into the compact form the server's
// export uses ("512mb", "1gb"), so a limit read from the daemon can be shown
// in the same field the user typed into.
export const formatBytes = (bytes) => {
  if (!bytes || bytes <= 0) return "";
  const GiB = 1024 * 1024 * 1024;
  const MiB = 1024 * 1024;
  const KiB = 1024;
  if (bytes % GiB === 0) return `${bytes / GiB}gb`;
  if (bytes % MiB === 0) return `${bytes / MiB}mb`;
  if (bytes % KiB === 0) return `${bytes / KiB}kb`;
  return `${bytes}b`;
};
