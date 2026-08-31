import React from 'react';
import { Alert, Checkbox, Chip, CircularProgress, FormControlLabel, Stack, Switch, Typography, useMediaQuery } from '@mui/material';
import MainCard from '../../../components/MainCard';
import * as API from '../../../api';
import NewDockerService from './newService';
import { PERM_CREDENTIALS_READ } from '../../../utils/permissions';
import { useClientInfos } from '../../../utils/hooks';
import { useTranslation } from 'react-i18next';

const ContainerComposeEdit = ({ containerInfo, config, refresh, updatesAvailable, selfName }) => {
  const isMobile = useMediaQuery((theme) => theme.breakpoints.down('sm'));
  const [isUpdating, setIsUpdating] = React.useState(false);
  const { hasPermission, hasRolePermission } = useClientInfos();
  const { t } = useTranslation();

  const { Name } = containerInfo;

  const [exportedCompose, setExportedCompose] = React.useState(null);
  // false = show the stored initial settings (what the user originally
  // configured), true = show the live Docker runtime values. The runtime view
  // externalizes settings the user never set (daemon defaults, image-provided
  // env, internal cosmos.* labels), so it is opt-in.
  const [showRuntime, setShowRuntime] = React.useState(false);
  const [hasStoredConfig, setHasStoredConfig] = React.useState(true);

  React.useEffect(() => {
    if (!hasPermission(PERM_CREDENTIALS_READ)) return;
    API.docker.exportContainer(Name.replace('/', ''), showRuntime ? 'runtime' : 'initial').then((res) => {
      setHasStoredConfig(res.stored !== false);
      setExportedCompose({
        services: {
          [Name.replace('/', '')]: res.data
        }
      });
    });
  }, [Name, showRuntime]);

  let refreshAll = refresh ? (() => refresh().then(() => {
    setIsUpdating(false);
  })) : (() => {setIsUpdating(false);});

  if (!hasPermission(PERM_CREDENTIALS_READ)) {
    return (
      <div style={{ maxWidth: '1000px', width: '100%', margin: 'auto', padding: '20px 0' }}>
        <Alert severity="warning">
          {hasRolePermission(PERM_CREDENTIALS_READ)
            ? t('mgmt.servapps.compose.credentialsRequired')
            : t('mgmt.servapps.compose.credentialsDenied')}
        </Alert>
      </div>
    );
  }

  return (
    <div style={{ maxWidth: '1000px', width: '100%', margin: 'auto' }}>
      <Stack direction="row" spacing={2} alignItems="center" justifyContent="space-between" sx={{ mb: 1 }}>
        <Typography variant="caption" color="text.secondary">
          {showRuntime
            ? t('mgmt.servapps.compose.runtimeValuesHint')
            : t('mgmt.servapps.compose.initialValuesHint')}
        </Typography>
        <FormControlLabel
          control={
            <Switch
              checked={showRuntime}
              onChange={(e) => setShowRuntime(e.target.checked)}
              size="small"
              disabled={!hasStoredConfig && !showRuntime}
            />
          }
          label={t('mgmt.servapps.compose.showRuntimeValues')}
        />
      </Stack>
      {!showRuntime && !hasStoredConfig && (
        <Alert severity="info" sx={{ mb: 1 }}>
          {t('mgmt.servapps.compose.noStoredConfig')}
        </Alert>
      )}
      {exportedCompose && <NewDockerService edit service={exportedCompose} refresh={refreshAll} />}
    </div>
  );
};

export default ContainerComposeEdit;
