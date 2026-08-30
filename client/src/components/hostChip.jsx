import { SettingOutlined } from "@ant-design/icons";
import { Chip } from "@mui/material";
import { useEffect, useState } from "react";
import { getOrigin, getFullOrigin, IsRouteSocketProxy } from "../utils/routes";
import { isContainerRunning } from "../utils/container-status";
import StatusDot from "./statusDot";

// Green: 2xx/3xx (incl. opaqueredirect for cross-origin login redirects) and
// the 4xx codes that mean the reverse proxy answered while refusing this
// caller/method (401, 403, 405, 407, 429, 511) - the service is up.
// Red: everything else (404, 408, 5xx, ...) and network errors.
function classifyProbeStatus(res) {
  if (res.type === 'opaqueredirect') return true;
  const s = res.status;
  if (s >= 200 && s < 400) return true;
  if (s === 401 || s === 403 || s === 405 || s === 407 || s === 429 || s === 511) return true;
  return false;
}

const HostChip = ({route, settings, container, style, ellipsis}) => {
  const [isOnline, setIsOnline] = useState(null);
  const url = getOrigin(route);

  // Socket proxies (e.g. 0.0.0.0:32400) have no HTTP layer to probe: show
  // green whenever the container runs, instead of firing a meaningless fetch.
  const isSocketProxy = route && IsRouteSocketProxy(route);

  useEffect(() => {
    if (!isContainerRunning(container)) {
      setIsOnline(null); // container not running: grey, nothing to probe
      return;
    }
    if (isSocketProxy) {
      setIsOnline(true);
      return;
    }
    // HEAD + cors exposes the real status; no-store bypasses stale caches.
    // redirect: 'manual' keeps 3xx (incl. login redirects) from being followed
    // into a CORS failure (e.g. a data: URL).
    fetch(getFullOrigin(route), {
      method: 'HEAD',
      mode: 'cors',
      cache: 'no-store',
      redirect: 'manual',
    }).then((res) => {
      setIsOnline(classifyProbeStatus(res));
    }).catch(() => {
      setIsOnline(false);
    });
  }, [url, container, isSocketProxy]);

  return <Chip
    label={<><StatusDot status={isOnline == null ? "unknown" : isOnline ? "success" : "error"} size={8} style={{ marginRight: 6 }} />{url}</>}
    color="primary"
    variant="outlined"
    style={{
      paddingRight: '4px',
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
