import wrap, { type ApiResponse, type ApiFetch } from './wrap';

export interface Deployment {
  name: string;
  replicas: number;
  compose: any;
}

export default function createDeploymentsAPI(apiFetch: ApiFetch) {
  function list(): Promise<ApiResponse<Record<string, Deployment>>> {
    return wrap(apiFetch('/cosmos/api/constellation/deployments', {
      method: 'GET',
      headers: { 'Content-Type': 'application/json' },
    }));
  }

  function get(name: string): Promise<ApiResponse<Deployment>> {
    return wrap(apiFetch('/cosmos/api/constellation/deployments/' + name, {
      method: 'GET',
      headers: { 'Content-Type': 'application/json' },
    }));
  }

  function create(values: Deployment): Promise<ApiResponse<Deployment>> {
    return wrap(apiFetch('/cosmos/api/constellation/deployments', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(values),
    }));
  }

  function update(name: string, values: Deployment): Promise<ApiResponse<Deployment>> {
    return wrap(apiFetch('/cosmos/api/constellation/deployments/' + name, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(values),
    }));
  }

  function remove(name: string): Promise<ApiResponse> {
    return wrap(apiFetch('/cosmos/api/constellation/deployments/' + name, {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
    }));
  }

  return { list, get, create, update, remove };
}
