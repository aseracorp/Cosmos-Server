import { SettingOutlined } from "@ant-design/icons";
import { Chip } from "@mui/material";
import { useEffect, useState } from "react";
import { getOrigin, getFullOrigin, IsRouteSocketProxy } from "../utils/routes";
import { useTheme } from '@mui/material/styles';
import StatusDot from "./statusDot";

// Classify a probe response into one of three states:
//   true  -> reachable (green): 2xx, 3xx, or 4xx codes that only mean "you are
//           not allowed / method not supported" (401 Unauthorized, 405 Method
//           Not Allowed) - the host is clearly up and answering.
//   false -> not reachable (red): 404 Not Found and 408 Request Timeout mean
//           there is no service answering at that path/port.
//   'unknown' -> everything else (4xx/5xx). We cannot tell for sure whether it
//           is up (e.g. 403 blocked, 429 rate-limited, 500/502/503 app error),
//           so signal an uncertain state (orange) instead of guessing.
function classifyProbeStatus(res) {
  if (res.type === 'opaqueredirect') {
    return true; // 3xx: the server answered (login redirect, etc.)
  }
  const s = res.status;
  if (s >= 200 && s < 400) return true;
  if (s === 401 || s === 405) return true; // auth required / method not allowed => up
  if (s === 404 || s === 408) return false; // not found / timeout => not available
  return 'unknown';
}

const HostChip = ({route, settings, container, style, ellipsis}) => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const [isOnline, setIsOnline] = useState(null);
  const url = getOrigin(route)

  // Only probe reachability when the container is actually running. When it is
  // stopped, paused, exited, ... there is nothing to reach, so show a neutral
  // (grey) dot instead of pinging and reporting a bogus state.
  // Resolve the raw run state from either the summary shape (State is a
  // string, used by the servapps list) or the inspect shape (State.Status,
  // used by the container overview). We deliberately use the raw run state
  // rather than the display status so that a healthy container (State.Status
  // "running" with a healthcheck) is still treated as running.
  let containerState = '';
  if (container) {
    if (typeof container.State === 'object' && container.State !== null) {
      containerState = container.State.Status || '';
    } else if (typeof container.State === 'string') {
      containerState = container.State;
    }
  }
  const containerRunning = !container || containerState === 'running';

  // Raw TCP/UDP socket proxies (e.g. 0.0.0.0:32400) have no HTTP layer to
  // probe, so an HTTP HEAD is meaningless. For these, "online" simply means
  // the container is running - show green instead of firing a bogus fetch.
  const isSocketProxy = route && IsRouteSocketProxy(route);

  useEffect(() => {
    if (!containerRunning) {
      setIsOnline(null);
      return;
    }
    if (isSocketProxy) {
      setIsOnline(true);
      return;
    }
    // Client-side probe: mode 'cors' (not no-cors) so the browser exposes the
    // real status. A no-cors response is opaque, so a proxied 502 (e.g. a
    // wrong downstream port) would still resolve and the dot would stay green.
    // The proxy adds Access-Control-Allow-Origin for HEAD requests (and
    // relaxes CORP), so the status is readable: <400 reachable, 4xx/5xx not.
    //
    // cache: 'no-store' forces the browser to bypass any cached HEAD response
    // (a stale pre-fix response without the right CORS headers could otherwise
    // be served from cache and surface as an intermittent CORS error).
    fetch(getFullOrigin(route), {
      method: 'HEAD',
      mode: 'cors',
      cache: 'no-store',
      // Do not follow redirects. A reachability dot only needs to know the
      // server answered; following redirects can fail CORS (e.g. an app that
      // redirects to a login page, or worse to a data: URL, which the browser
      // rejects as "CORS request was not http"). With redirect: 'manual', a
      // 3xx (including an opaqueredirect for cross-origin redirects) resolves
      // here instead of being followed, and counts as reachable.
      redirect: 'manual',
    }).then((res) => {
      const result = classifyProbeStatus(res);
      // true -> green, false -> red, 'unknown' -> orange (via warning)
      setIsOnline(result === true ? true : result === false ? false : 'unknown');
    }).catch(() => {
      setIsOnline(false);
    });
  }, [url, containerRunning, isSocketProxy]);

  return <Chip
    label={<><StatusDot status={isOnline == null ? "unknown" : isOnline === true ? "success" : isOnline === false ? "error" : "warning"} size={8} style={{ marginRight: 6 }} />{url}</>}
    color="primary"
    variant="outlined"
    style={{
      paddingRight: '4px',
      // textDecoration: isOnline ? 'none' : 'underline wavy red',
      ...style,
      ...(ellipsis ? { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: '250px' } : {})
    }}
    onClick={() => {
      if(route.UseHost)
        window.open(window.location.origin.split("://")[0] + "://" + route.Host + route.PathPrefix, '_blank');
      else
        window.open(window.location.origin + route.PathPrefix, '_blank');
    }}
    onDelete={settings ? () => {
      window.open('/cosmos-ui/config-url/'+route.Name, '_blank');
    } : null}
    deleteIcon={settings ? <SettingOutlined /> : null}
  />
}

export default HostChip;