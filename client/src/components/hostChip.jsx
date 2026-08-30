import { SettingOutlined } from "@ant-design/icons";
import { Chip } from "@mui/material";
import { useEffect, useState } from "react";
import { getOrigin, getFullOrigin } from "../utils/routes";
import { useTheme } from '@mui/material/styles';
import StatusDot from "./statusDot";

const HostChip = ({route, settings, style, ellipsis}) => {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const [isOnline, setIsOnline] = useState(null);
  const url = getOrigin(route)

  useEffect(() => {
    // Client-side probe: mode 'cors' (not no-cors) so the browser exposes the
    // real status; no-store bypasses stale cached responses; redirect 'manual'
    // keeps 3xx (login redirects etc.) from being followed into a CORS failure.
    fetch(getFullOrigin(route), {
      method: 'HEAD',
      mode: 'cors',
      cache: 'no-store',
      redirect: 'manual',
    }).then((res) => {
      // Green: 2xx/3xx (incl. opaqueredirect) and 401/403/405/407/429/511
      // (the reverse proxy answered while refusing this caller - host is up).
      // Red: everything else (404, 408, 5xx, ...) and network errors.
      setIsOnline(
        res.type === 'opaqueredirect' ||
        (res.status >= 200 && res.status < 400) ||
        res.status === 401 || res.status === 403 || res.status === 405 ||
        res.status === 407 || res.status === 429 || res.status === 511
      );
    }).catch(() => {
      setIsOnline(false);
    });
  }, [url]);

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
