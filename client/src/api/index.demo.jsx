
export const getStatus = () => {
  return new Promise((resolve, reject) => {
    resolve({
      "data": {
        "AVX": true,
        "CPU": "amd64",
        "HTTPSCertificateMode": "LETSENCRYPT",
        "LetsEncryptErrors": [],
        "MonitoringDisabled": false,
        "backup_status": "",
        "database": true,
        "docker": true,
        "domain": false,
        "Licence": true,
        "CACert": true,
        "hostmode": "true",
        "homepage": {
          "Background": "/cosmos/api/background/avif",
          "Widgets": null,
          "Expanded": false
        },
        "LicenceNumber": 20,
        "LicenceNodeNumber": 5,
        "hostname": "yann-server.com",
        "letsencrypt": false,
        "needsRestart": false,
        "newVersionAvailable": false,
        "resources": {},
        "theme": {
          "PrimaryColor": "rgba(191, 100, 64, 1)",
          "SecondaryColor": ""
        }
      },
      "status": "OK"
    });
  });
}

export const isOnline = () => {
  return new Promise((resolve, reject) => {
    setTimeout(() => {
      resolve({
        "status": "ok",
      })},
      2000
    );
  });
}

export const newInstall = (req) => {
  return new Promise((resolve, reject) => {
    setTimeout(() => {
      resolve({
        "status": "ok",
      })},
      2000
    );
  });
}

export const getDNS = (host) => (req) => {
  return new Promise((resolve, reject) => {
    setTimeout(() => {
      resolve({
        "status": "ok",
        "data": "199.199.199.199"
      })},
      100
    );
  });
}

export const checkHost = (host) => {
  return new Promise((resolve, reject) => {
    setTimeout(() => {
      resolve({
        "status": "ok",
        "data": "199.199.199.199"
      })},
      100
    );
  });
}

export const uploadImage = (file) => {
  return new Promise((resolve, reject) => {
    setTimeout(() => {
      resolve({
        "status": "ok",
        "data": ""
      })}, 100 );
    });
  }

export const terminal = () => ({
  send: (data) => {
    onmessage("This is a demo, what did you expect?");
  },
  close: ()=>{}
});
export const desecSetup = (req) => {
  return new Promise((resolve) => setTimeout(() => resolve({
    "status": "ok",
    "data": { "activationState": "active" }
  }), 1000));
}

export const desecSetupStatus = () => {
  return new Promise((resolve) => setTimeout(() => resolve({
    "status": "ok",
    "data": { "activationState": "active" }
  }), 1000));
}

export const desecSetupCaptcha = () => {
  return new Promise((resolve) => setTimeout(() => resolve({
    "status": "ok",
    "data": { "id": "demo-captcha-id", "challenge": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAFZ4J8w==" }
  }), 1000));
}

export const ddns = (req) => {
  return new Promise((resolve) => setTimeout(() => resolve({
    "status": "ok",
    "data": { "enabled": false }
  }), 500));
}

export const networkDetect = () => {
  return new Promise((resolve) => setTimeout(() => resolve({
    "status": "ok",
    "data": { "publicIp": "199.199.199.199", "isCGNAT": false, "upnpAvailable": true, "routerVendor": "AVM", "lanIp": "192.168.1.100" }
  }), 1000));
}

export const upnp = (action) => {
  return new Promise((resolve) => setTimeout(() => resolve({ "status": "ok" }), 500));
}
