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
const probeRoute = async (route, isSocketProxy) => {
  if (isSocketProxy) {
    // Raw TCP/UDP socket proxies have no HTTP layer to probe; online simply
    // means the container is running (handled by the container prop gating).
    return { sleeping: false };
  }
  const origin = getFullOrigin(route);
  try {
    const res = await fetch(origin + (origin.includes('?') ? '&' : '?') + '__cosmos_probe=1', {
      method: 'HEAD',
      mode: 'cors',
      credentials: 'include',
      redirect: 'manual',
      cache: 'no-store',
    });
    return {
      sleeping: res.headers.get('X-Cosmos-Container') === 'sleeping',
      online: true,
    };
  } catch (e) {
    try {
      // CORS-shielded failure: the proxy answered but kept the headers hidden.
      await fetch(origin, { method: 'HEAD', mode: 'no-cors' });
      return { sleeping: false, online: true };
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
    probeRoute(route, isSocketProxy).then((s) => { if (!cancelled) setStatus(s); });
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
