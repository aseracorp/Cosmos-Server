import { SettingOutlined } from "@ant-design/icons";
import { Chip, Tooltip } from "@mui/material";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { getOrigin, getFullOrigin, IsRouteSocketProxy } from "../utils/routes";
import { isContainerRunning } from "../utils/container-status";
import StatusDot from "./statusDot";

// Probes the route through the Cosmos reverse proxy without waking a lazy
// (sleeping) container: the proxy answers the __cosmos_probe HEAD request
// itself with X-Cosmos-Container: sleeping when the container is dormant.
//
// A response is "online" only when we get a _successful_ HTTP status back.
// Any error, 4xx/5xx (bookmarked/dead route), or redirect-to-nowhere must
// show as offline. CORS failures alone are NOT proof of offline: retried
// with an opaque (no-cors) request that itself succeeds.
const probeRoute = async (route) => {
  const origin = getFullOrigin(route);
  const probeUrl = origin + (origin.includes('?') ? '&' : '?') + '__cosmos_probe=1';

  // Primary probe: same-origin CORS request so we can read the header.
  try {
    const res = await fetch(probeUrl, {
      method: 'HEAD',
      mode: 'cors',
      credentials: 'include',
      redirect: 'manual',
      cache: 'no-store',
    });
    // NOTE: Do NOT treat a transparent redirect as failure here. The proxy
    // answers __cosmos_probe itself for the origin path; if it returns a
    // redirect opaquely we still consider the proxy reachable.
    return {
      sleeping: res.headers.get('X-Cosmos-Container') === 'sleeping',
      online: res.ok, // 2xx => online; 403/404/5xx => offline
    };
  } catch (e) {
    // CORS error might mean the proxy answered but hid the headers (e.g.
    // cross-origin route, header-hardening). Fall back to an opaque HEAD
    // that reports reachability without exposing status.
    try {
      const opaque = await fetch(origin, { method: 'HEAD', mode: 'no-cors', cache: 'no-store' });
      // An opaque response with status 0 can still be a real 403/404: probe
      // again with a no-cors GET to a probe path that the app itself must
      // 404/200 on (most servers respond to both). Keep it conservative.
      const opaque2 = await fetch(probeUrl, { method: 'HEAD', mode: 'no-cors', cache: 'no-store' });
      return {
        sleeping: false,
        online: opaque && opaque2 ? true : false,
      };
    } catch (e2) {
      return { sleeping: false, online: false };
    }
  }
};

const HostChip = ({route, settings, container, style, ellipsis}) => {
  const { t } = useTranslation();
  const [status, setStatus] = useState(null); // null | { sleeping: bool, online: bool }
  const url = getOrigin(route);
  const isSocketProxy = route && IsRouteSocketProxy(route);

  // Container run state gating: when a container is passed and it is not
  // actually running, show a grey dot and do not probe (e.g. the app is
  // stopped). Socket proxies show green once the container is running (no
  // HTTP layer to probe).
  const containerDown = container && !isContainerRunning(container);

  useEffect(() => {
    if (containerDown) {
      setStatus({ sleeping: false, online: null });
      return;
    }
    if (isSocketProxy) {
      setStatus({ sleeping: false, online: true });
      return;
    }
    let cancelled = false;
    probeRoute(route).then((s) => { if (!cancelled) setStatus(s); });
    return () => { cancelled = true; };
  }, [url, containerDown, isSocketProxy, route]);

  const dot = status && status.sleeping
    ? <Tooltip title={t('global.containerSleeping')}><StatusDot status="unknown" hollow size={8} style={{ marginRight: 6 }} /></Tooltip>
    : <StatusDot
        status={!status || status.online === null ? "unknown" : status.online ? "success" : "error"}
        size={8}
        style={{ marginRight: 6 }}
      />;

  return <Chip
    label={<>{dot}{url}</>}
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
