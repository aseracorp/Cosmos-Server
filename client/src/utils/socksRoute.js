// Route-level SOCKS relay helpers.
//
// Mirrors src/proxy/Socks5Policy.go on the client. Kept as a pure module (no JSX,
// no React, no i18n imports) so the rules are executable and reviewable in
// isolation, and so the editor stays a thin renderer of these decisions.
//
// The server is the source of truth; this module exists to show the same rules
// before a save, not to enforce them.

// The one scheme a SOCKS relay route can use.
//
// socks5h (--socks5-hostname) is deliberately absent: it asks Cosmos to resolve
// the destination hostname, which is a forward-proxy feature. Cosmos is a
// reverse proxy, and every client that reaches a route has already resolved the
// name, so a relay here dials addresses and never resolves. socks5t is absent
// too: no URL parser, curl, browser or library accepts it, and the relay is
// TCP-only, so there is no UDP capability for a scheme to signal.
const SCHEMES = ['socks5://'];

/** True when this route is a SOCKS relay (target scheme selects the feature). */
export const isSocksTarget = (target) => {
  if (!target || typeof target !== 'string') return false;
  return SCHEMES.some((s) => target.toLowerCase().startsWith(s));
};

const splitList = (value) => {
  if (Array.isArray(value)) return value;
  if (typeof value === 'string') return value.split(',');
  return [];
};

/** Normalise a comma-separated field into a clean array, or [] when empty. */
export const normalizeDestinationList = (value) => {
  return splitList(value)
    .map((s) => String(s).trim())
    .filter((s) => s.length > 0);
};

/**
 * A destination entry is an IPv4/IPv6 address or a CIDR. Host names are not
 * entries: the relay dials the address the client sent and never resolves, so a
 * hostname rule would look like protection in the editor and match nothing at
 * runtime. Deliberately strict, matching the backend, so the user is told
 * before saving rather than at bind time.
 */
export const destinationEntryIsInvalid = (entry) => {
  const s = String(entry || '').trim();
  if (s.length === 0) return false; // dropped by normalizeDestinationList
  if (/\s/.test(s)) return true; // spaces mean a paste of several entries
  if (s.includes('/')) {
    const parts = s.split('/');
    if (parts.length !== 2) return true;
    const [addr, prefix] = parts;
    if (!/^[0-9]+$/.test(prefix)) return true;
    const n = Number(prefix);
    // The ceiling depends on the address family: net.ParseCIDR rejects 10.0.0.0/33
    // outright, so the editor has to as well.
    const max = addr.includes(':') ? 128 : 32;
    if (n < 0 || n > max) return true;
    return ipLiteralIsInvalid(addr);
  }
  if (s.includes(':')) return ipLiteralIsInvalid(s);
  // Dotted-quad-shaped entries are IPv4 literals, not host names: without this
  // split, "300.1.1.1" reads as a legal hostname and the backend rejects it at
  // bind time -- after the user already saved.
  if (/^[0-9.]+$/.test(s)) return ipLiteralIsInvalid(s);
  // Anything else with a dot is a host name, which this relay cannot enforce.
  return true;
};

const ipLiteralIsInvalid = (addr) => {
  if (addr.includes(':')) {
    // IPv6: accepts the compressed forms, which is all a user writes by hand.
    const groups = addr.split(':');
    if (groups.length < 3 || groups.length > 9) return true;
    return groups.some((g) => g.length > 0 && !/^[0-9a-f]{1,4}$/i.test(g));
  }
  const parts = addr.split('.');
  if (parts.length !== 4) return true;
  return parts.some((p) => !/^[0-9]{1,3}$/.test(p) || Number(p) > 255);
};

/** Invalid entries of a destination list, as an array (empty when all valid). */
export const invalidDestinationEntries = (value) => {
  return normalizeDestinationList(value).filter(destinationEntryIsInvalid);
};

/**
 * True when the route can act as a SOCKS relay: a socks target on a listener
 * mode. Mirrors IsRouteSocketProxy from utils/routes.jsx, which is what the
 * backend uses to decide a route becomes a SocketProxy listener.
 */
export const isSocksRelayRoute = (route) => {
  if (!route || !route.Target) return false;
  if (route.Mode !== 'PROXY' && route.Mode !== 'SERVAPP') return false;
  return isSocksTarget(route.Target);
};

const SOCKS_FIELDS = [
  'SocksAuthMode',
  'SocksAllowDestinations',
  'SocksDenyDestinations',
];

// Wire keys are the Go field names, not the YAML keys: route config is
// marshalled through encoding/json, which is case-insensitive but does NOT
// ignore underscores. Sending socks_auth_mode would decode into nothing and the
// relay would silently run with defaults.
//
// SocksAuthMode is a string on the server, so it is emitted as a string -- a
// JSON boolean would fail to unmarshal into a string field and reject the
// entire route save.
export const SOCKS_JSON_KEYS = ['SocksAuthMode', 'SocksAllowDestinations', 'SocksDenyDestinations'];

/**
 * Normalise SOCKS fields before a save and strip them from routes that are not
 * relays, so changing the target scheme away from socks does not leave dead
 * options behind in the config.
 *
 * Tri-state fields are dropped when empty rather than sent as '': the server
 * derives defaults from the scheme, and an explicit '' would be noise.
 */
export const sanitizeSocksFields = (route) => {
  const out = { ...route };

  if (!isSocksRelayRoute(out)) {
    for (const field of SOCKS_FIELDS) delete out[field];
    return out;
  }

  if (out.SocksAuthMode === 'none' || out.SocksAuthMode === 'password') {
    out.SocksAuthMode = out.SocksAuthMode;
  } else {
    delete out.SocksAuthMode;
  }

  out.SocksAllowDestinations = normalizeDestinationList(out.SocksAllowDestinations);
  out.SocksDenyDestinations = normalizeDestinationList(out.SocksDenyDestinations);
  if (out.SocksAllowDestinations.length === 0) delete out.SocksAllowDestinations;
  if (out.SocksDenyDestinations.length === 0) delete out.SocksDenyDestinations;

  return out;
};
