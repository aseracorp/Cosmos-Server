import wrap, { type ApiResponse, type ApiFetch } from './wrap';

// Cosmos CI (Pro): git-connected builds. A project connects a repository; every
// push becomes a build run by a node; the artifact (image, static site, function
// package) is deployed through the project's deploy rule or the repo's cosmos.json.

export interface CISource {
  provider: 'github' | 'gitlab' | 'gitea' | 'bitbucket' | 'git' | string;
  repoUrl: string;
  apiUrl?: string;
  // Write-only: the API answers "****" when set.
  token?: string;
  username?: string;
  rootDir?: string;
  branches?: string[];
  defaultBranch?: string;
}

export interface CIBuildSettings {
  strategy?: '' | 'railpack' | 'dockerfile' | 'static' | 'woodpecker' | 'none';
  dockerfile?: string;
  publishDir?: string;
  env?: Record<string, string>;
  platform?: string;
  timeoutMinutes?: number;
}

export interface CIRegistryTarget {
  registry?: string;
  image?: string;
  staticRegistry?: string;
  tokens?: Record<string, string>;
}

export interface CIEnvironment {
  name: string;
  deployment?: string;
  host?: string;
  autoDeploy: boolean;
}

export interface CIDeployTemplate {
  replicas?: number;
  port?: number;
  host?: string;
  env?: Record<string, string>;
  tags?: string[];
  strategy?: string;
  healthcheck?: string[];
  memLimit?: number;
  spa?: boolean;
  internal?: boolean;
}

export interface CIDeployRule {
  enabled: boolean;
  type?: 'container' | 'static' | 'function';
  name?: string;
  environments?: Record<string, CIEnvironment>;
  pullRequests: boolean;
  previewTtl?: string;
  previewHost?: string;
  template: CIDeployTemplate;
}

export interface CISecret {
  name: string;
  // Write-only: empty on read, empty on write = keep the stored value.
  value?: string;
  availableToPRs: boolean;
  updatedAt?: string;
}

export interface CIWebhook {
  secret: string;
  providerHookId?: string;
  registered: boolean;
  error?: string;
  lastDeliveryAt?: string;
}

export interface CIPreview {
  pr: number;
  branch: string;
  deployment: string;
  host?: string;
  build: number;
  createdAt: string;
  expiresAt: string;
}

export interface CIProjectStats {
  succeeded: number;
  failed: number;
  lastStatus?: string;
  lastBuild?: number;
  lastBuildAt?: string;
  lastDurationMs?: number;
}

export interface CIProject {
  name: string;
  description?: string;
  source: CISource;
  build: CIBuildSettings;
  deploy: CIDeployRule;
  registry: CIRegistryTarget;
  secrets?: CISecret[];
  trust: { pullRequests: 'off' | 'collaborators' | 'nosecrets' | 'approval' | string };
  tags?: string[];
  enabled: boolean;
  webhook: CIWebhook;
  lastBuildNumber: number;
  previews?: CIPreview[];
  stats: CIProjectStats;
  createdAt: string;
  updatedAt: string;
  // Filled by the server.
  webhookUrl: string;
  imageRepo?: string;
}

export interface CITrigger {
  event: 'push' | 'pull_request' | 'manual' | 'retry' | 'rollback' | string;
  ref?: string;
  branch?: string;
  sha?: string;
  message?: string;
  author?: string;
  authorEmail?: string;
  actor?: string;
  pr?: number;
  prTitle?: string;
  prSourceBranch?: string;
  prTargetBranch?: string;
  prAction?: string;
  prAuthor?: string;
  retryOf?: number;
}

export interface CIBuildStep {
  name: string;
  kind: 'clone' | 'ci' | 'build' | 'publish' | 'deploy' | 'service' | string;
  image?: string;
  status: 'pending' | 'running' | 'success' | 'failed' | 'skipped' | 'canceled' | string;
  startedAt?: string;
  finishedAt?: string;
  durationMs?: number;
  exitCode?: number;
  error?: string;
  logChunks: number;
  logBytes: number;
}

export interface CIArtifact {
  type: 'image' | 'static' | 'package' | string;
  ref?: string;
  registry?: string;
  image?: string;
  tag?: string;
  digest?: string;
  site?: string;
  version?: string;
  size?: number;
}

export interface CIDeployResult {
  type: string;
  name: string;
  environment?: string;
  action: 'created' | 'updated' | 'none' | 'failed' | string;
  version?: number;
  preview?: boolean;
  url?: string;
  error?: string;
}

export interface CIBuild {
  project: string;
  number: number;
  status: 'queued' | 'pending-approval' | 'running' | 'success' | 'failed' | 'canceled' | 'skipped' | string;
  trigger: CITrigger;
  strategy?: string;
  configSource?: string;
  node?: string;
  queuedAt: string;
  startedAt?: string;
  finishedAt?: string;
  heartbeatAt?: string;
  durationMs?: number;
  steps: CIBuildStep[];
  artifact?: CIArtifact;
  deployment?: CIDeployResult;
  environment?: string;
  error?: string;
  withSecrets: boolean;
  approvedBy?: string;
  warnings?: string[];
}

export interface CIDetection {
  strategy: string;
  configSource: string;
  dockerfile?: string;
  publishDir?: string;
  woodpeckerFile?: string;
  cosmos?: any;
  cosmosRaw?: string;
  railpackProviders?: string[];
  folders?: string[];
  files?: string[];
  sha?: string;
  branch?: string;
  commitMessage?: string;
  commitAuthor?: string;
  warnings?: string[];
}

export interface CIRunner {
  node: string;
  tags: string[];
  running: number;
  max: number;
  buildkitd: string;
  reachable: boolean;
}

export interface CIStepLogs {
  step: CIBuildStep;
  text: string;
  next: number;
  done: boolean;
  status: string;
}

export default function createCIAPI(apiFetch: ApiFetch) {
  const base = '/cosmos/api/constellation/ci';
  const json = { 'Content-Type': 'application/json' };
  const project = (name: string) => base + '/projects/' + encodeURIComponent(name);
  const build = (name: string, number: number) => project(name) + '/builds/' + number;

  function list(): Promise<ApiResponse<CIProject[]>> {
    return wrap(apiFetch(base + '/projects', { method: 'GET', headers: json }));
  }

  function get(name: string): Promise<ApiResponse<CIProject>> {
    return wrap(apiFetch(project(name), { method: 'GET', headers: json }));
  }

  function create(values: Partial<CIProject>): Promise<ApiResponse<CIProject> & { warning?: string }> {
    return wrap(apiFetch(base + '/projects', { method: 'POST', headers: json, body: JSON.stringify(values) }));
  }

  function update(name: string, values: Partial<CIProject>): Promise<ApiResponse<CIProject> & { warning?: string }> {
    return wrap(apiFetch(project(name), { method: 'PUT', headers: json, body: JSON.stringify(values) }));
  }

  function remove(name: string): Promise<ApiResponse> {
    return wrap(apiFetch(project(name), { method: 'DELETE', headers: json }));
  }

  function webhook(name: string, action: 'rotate' | 'register'): Promise<ApiResponse<CIProject>> {
    return wrap(apiFetch(project(name) + '/webhook/' + action, { method: 'POST', headers: json }));
  }

  function detect(req: { source: Partial<CISource>; build?: CIBuildSettings; project?: string }): Promise<ApiResponse<CIDetection> & { defaultBranch?: string; provider?: string }> {
    return wrap(apiFetch(base + '/detect', { method: 'POST', headers: json, body: JSON.stringify(req) }));
  }

  function builds(name: string, limit?: number): Promise<ApiResponse<CIBuild[]>> {
    return wrap(apiFetch(project(name) + '/builds' + (limit ? '?limit=' + limit : ''), { method: 'GET', headers: json }));
  }

  function allBuilds(limit?: number): Promise<ApiResponse<CIBuild[]>> {
    return wrap(apiFetch(base + '/builds' + (limit ? '?limit=' + limit : ''), { method: 'GET', headers: json }));
  }

  function run(name: string, req: { branch?: string; sha?: string }): Promise<ApiResponse<CIBuild>> {
    return wrap(apiFetch(project(name) + '/builds', { method: 'POST', headers: json, body: JSON.stringify(req || {}) }));
  }

  function getBuild(name: string, number: number): Promise<ApiResponse<CIBuild>> {
    return wrap(apiFetch(build(name, number), { method: 'GET', headers: json }));
  }

  function removeBuild(name: string, number: number): Promise<ApiResponse> {
    return wrap(apiFetch(build(name, number), { method: 'DELETE', headers: json }));
  }

  function action(name: string, number: number, act: 'cancel' | 'retry' | 'approve' | 'deploy'): Promise<ApiResponse<CIBuild>> {
    return wrap(apiFetch(build(name, number) + '/' + act, { method: 'POST', headers: json }));
  }

  function logs(name: string, number: number, step: number, from: number): Promise<ApiResponse<CIStepLogs>> {
    return wrap(apiFetch(build(name, number) + '/logs?step=' + step + '&from=' + from, { method: 'GET', headers: json }));
  }

  function runners(): Promise<ApiResponse<CIRunner[]>> {
    return wrap(apiFetch(base + '/runners', { method: 'GET', headers: json }));
  }

  return { list, get, create, update, remove, webhook, detect, builds, allBuilds, run, getBuild, removeBuild, action, logs, runners };
}
