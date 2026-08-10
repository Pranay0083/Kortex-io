import { AxiosError } from "axios";
import { api } from "@/lib/api";

/**
 * Route the shared axios instance through an in-memory handler.
 * `routes` maps "METHOD /path" (path relative to /api, without query) to
 * either [status, body] or a function (config) => [status, body] | Promise<...>.
 * Returns the list of calls made, for assertions.
 */
export function mockApi(routes) {
  const calls = [];
  const original = api.defaults.adapter;
  api.defaults.adapter = async (config) => {
    const path = (config.url || "").split("?")[0];
    const key = `${(config.method || "get").toUpperCase()} ${path}`;
    calls.push({ key, url: config.url, data: config.data, config });
    let handler = routes[key];
    if (handler === undefined) handler = [404, { detail: `no mock for ${key}` }];
    const [status, data] = typeof handler === "function" ? await handler(config) : handler;
    const response = { data, status, statusText: String(status), headers: {}, config, request: {} };
    if (status >= 200 && status < 300) return response;
    throw new AxiosError(
      `Request failed with status code ${status}`,
      status >= 500 ? AxiosError.ERR_BAD_RESPONSE : AxiosError.ERR_BAD_REQUEST,
      config,
      {},
      response,
    );
  };
  calls.restore = () => {
    api.defaults.adapter = original;
  };
  return calls;
}

export const countCalls = (calls, key) => calls.filter((c) => c.key === key).length;
