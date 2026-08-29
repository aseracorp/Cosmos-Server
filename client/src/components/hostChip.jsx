import { SettingOutlined } from "@ant-design/icons";
import { Chip } from "@mui/material";
import { useEffect, useState } from "react";
import { getOrigin, getFullOrigin } from "../utils/routes";
import { useTheme } from '@mui/material/styles';
import StatusDot from "./statusDot";

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

  useEffect(() => {
    if (!containerRunning) {
      setIsOnline(null);
      return;
    }
    // Client-side probe: mode 'cors' (not no-cors) so the browser exposes the
    // real status. A no-cors response is opaque, so a proxied 502 (e.g. a
    // wrong downstream port) would still resolve and the dot would stay green.
    // The proxy adds Access-Control-Allow-Origin for HEAD requests (and
    // relaxes CORP), so the status is readable: <400 reachable, 4xx/5xx not.
    fetch(getFullOrigin(route), {
      method: 'HEAD',
      mode: 'cors',
    }).then((res) => {
      setIsOnline(res.status >= 200 && res.status < 400);
    }).catch(() => {
      setIsOnline(false);
    });
  }, [url, containerRunning]);

  return <Chip
    label={<><StatusDot status={isOnline == null ? "unknown" : isOnline ? "success" : "error"} size={8} style={{ marginRight: 6 }} />{url}</>}
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