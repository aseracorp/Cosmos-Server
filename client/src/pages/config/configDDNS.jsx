import * as React from 'react';
import MainCard from '../../components/MainCard';
import {
  Alert,
  Button,
  Grid,
  Stack,
  Typography,
} from '@mui/material';
import { CosmosCheckbox, CosmosInputPassword, CosmosInputText } from './users/formShortcuts';
import { useTranslation } from 'react-i18next';
import * as API from '../../api';
import { SyncOutlined, SearchOutlined } from '@ant-design/icons';

const ConfigDDNS = ({ formik }) => {
  const { t } = useTranslation();
  const [net, setNet] = React.useState(null);
  const [netLoading, setNetLoading] = React.useState(false);
  const [upnpMsg, setUpnpMsg] = React.useState(null);
  const [ddnsTest, setDdnsTest] = React.useState(null);

  const detect = () => {
    setNetLoading(true);
    API.networkDetect().then((res) => {
      setNet(res.data);
      setNetLoading(false);
    }).catch(() => setNetLoading(false));
  };

  const upnpAction = (action) => {
    setUpnpMsg(null);
    API.upnp(action).then(() => {
      setUpnpMsg({ type: 'success', text: action === 'add' ? t('mgmt.config.ddns.upnpAdded') : t('mgmt.config.ddns.upnpRemoved') });
    }).catch((e) => {
      setUpnpMsg({ type: 'error', text: e.message || t('mgmt.config.ddns.upnpError') });
    });
  };

  const testNow = () => {
    setDdnsTest(null);
    API.ddns({
      enabled: formik.values.DDNS_Enabled,
      fqdn: formik.values.DDNS_FQDN,
      token: formik.values.DDNS_Token === '***' ? '' : formik.values.DDNS_Token,
    }).then(() => {
      setDdnsTest({ type: 'success', text: t('mgmt.config.ddns.testOk') });
    }).catch((e) => {
      setDdnsTest({ type: 'error', text: e.message || t('mgmt.config.ddns.testError') });
    });
  };

  return (
    <Stack spacing={3}>
      <MainCard title={t('mgmt.config.ddns.title')}>
        <Stack spacing={2}>
          <Typography variant="body2">{t('mgmt.config.ddns.intro')}</Typography>
          <CosmosCheckbox
            label={t('mgmt.config.ddns.enabledLabel')}
            name="DDNS_Enabled"
            formik={formik}
          />
          <CosmosInputText
            label={t('mgmt.config.ddns.fqdnLabel')}
            name="DDNS_FQDN"
            formik={formik}
            placeholder={'vpn.mybox.dedyn.io'}
          />
          <CosmosInputPassword
            label={t('mgmt.config.ddns.tokenLabel')}
            name="DDNS_Token"
            formik={formik}
            placeholder={t('mgmt.config.ddns.tokenPlaceholder')}
          />
          <Stack direction="row" spacing={2}>
            <Button variant="outlined" startIcon={<SyncOutlined />} onClick={testNow}>
              {t('mgmt.config.ddns.testNow')}
            </Button>
          </Stack>
          {ddnsTest && <Alert severity={ddnsTest.type}>{ddnsTest.text}</Alert>}
          <Typography variant="caption">{t('mgmt.config.ddns.help')}</Typography>
        </Stack>
      </MainCard>

      <MainCard title={t('mgmt.config.ddns.routerTitle')}>
        <Stack spacing={2}>
          <Button variant="outlined" startIcon={<SearchOutlined />} onClick={detect} disabled={netLoading}>
            {netLoading ? t('mgmt.config.ddns.detecting') : t('mgmt.config.ddns.detectNetwork')}
          </Button>

          {net && (
            <Stack spacing={1}>
              <Alert severity={net.isCGNAT ? 'warning' : 'success'}>
                {net.isCGNAT ? t('mgmt.config.ddns.cgnatDetected') : t('mgmt.config.ddns.publicIpDetected')}
                {net.publicIp ? ` (${net.publicIp})` : ''}
              </Alert>
              {net.lanIp && <Typography variant="body2">{t('mgmt.config.ddns.lanIp')}: {net.lanIp}</Typography>}
              {net.upnpAvailable ? (
                <Typography variant="body2">{t('mgmt.config.ddns.upnpAvailable')}</Typography>
              ) : (
                <Typography variant="body2">{t('mgmt.config.ddns.upnpUnavailable')}</Typography>
              )}
              {net.routerVendor && <Typography variant="body2">{t('mgmt.config.ddns.routerVendor')}: {net.routerVendor} {net.routerModel || ''}</Typography>}
              <Stack direction="row" spacing={2}>
                <Button variant="contained" color="primary" onClick={() => upnpAction('add')}>
                  {t('mgmt.config.ddns.upnpAdd')}
                </Button>
                <Button variant="outlined" color="secondary" onClick={() => upnpAction('remove')}>
                  {t('mgmt.config.ddns.upnpRemove')}
                </Button>
              </Stack>
              {upnpMsg && <Alert severity={upnpMsg.type}>{upnpMsg.text}</Alert>}
              {net.isCGNAT && (
                <Alert severity="info">
                  {t('mgmt.config.ddns.cgnatGuide')}{' '}
                  <a target="_blank" rel="noopener noreferrer" href="https://cosmos-cloud.io/docs">{t('mgmt.config.ddns.cosmosDocs')}</a>
                </Alert>
              )}
            </Stack>
          )}
        </Stack>
      </MainCard>
    </Stack>
  );
};

export default ConfigDDNS;