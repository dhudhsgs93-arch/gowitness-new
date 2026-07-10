import { gallery, list, statistics, wappalyzer, detail, searchresult, technologylist, review, reviewStats, trashedHost, category, domainEntry } from "@/lib/api/types";

const endpoints = {
  // api base path
  base: {
    path: import.meta.env.VITE_GOWITNESS_API_BASE_URL
      ? import.meta.env.VITE_GOWITNESS_API_BASE_URL + `/api`
      : `/api`,
    returnas: [] // n/a
  },
  // screenshot path
  screenshot: {
    path: import.meta.env.VITE_GOWITNESS_API_BASE_URL
      ? import.meta.env.VITE_GOWITNESS_API_BASE_URL + `/screenshots`
      : `/screenshots`,
    returnas: [] // n/a
  },

  // get endpoints
  statistics: {
    path: `/statistics`,
    returnas: {} as statistics
  },
  wappalyzer: {
    path: `/wappalyzer`,
    returnas: {} as wappalyzer
  },
  gallery: {
    path: `/results/gallery`,
    returnas: {} as gallery
  },
  list: {
    path: `/results/list`,
    returnas: [] as list[]
  },
  detail: {
    path: `/results/detail/:id`,
    returnas: {} as detail
  },
  technology: {
    path: `/results/technology`,
    returnas: {} as technologylist
  },

  // review endpoints
  reviewGet: {
    path: `/review/:id`,
    returnas: {} as review
  },
  reviewSet: {
    path: `/review/:id`,
    returnas: {} as { ok: boolean }
  },
  reviewBulk: {
    path: `/review/bulk`,
    returnas: {} as { ok: boolean; count: number }
  },
  reviewStats: {
    path: `/review/stats`,
    returnas: {} as reviewStats
  },
  autoTag: {
    path: `/review/auto-tag`,
    returnas: {} as { scanned: number; tagged: number; counts: Record<string, number> }
  },

  // trash endpoints
  trashList: {
    path: `/trash`,
    returnas: [] as trashedHost[]
  },
  trashAdd: {
    path: `/trash`,
    returnas: {} as { ok: boolean; trashed_host: trashedHost }
  },
  trashBulk: {
    path: `/trash/bulk`,
    returnas: {} as { ok: boolean; hosts: number; added: number }
  },
  trashRestore: {
    path: `/trash/restore`,
    returnas: {} as { ok: boolean }
  },
  trashSuggest: {
    path: `/trash/suggest`,
    returnas: [] as string[]
  },

  // category endpoints
  categories: {
    path: `/categories`,
    returnas: [] as category[]
  },
  categoryCreate: {
    path: `/categories`,
    returnas: {} as category
  },
  categoryDelete: {
    path: `/categories/delete`,
    returnas: {} as { ok: boolean }
  },
  categoryDomains: {
    path: `/categories/domains`,
    returnas: [] as domainEntry[]
  },
  categoryAssign: {
    path: `/categories/assign`,
    returnas: {} as { ok: boolean; count: number }
  },
  categoryUnassign: {
    path: `/categories/unassign`,
    returnas: {} as { ok: boolean; count: number }
  },

  // post endpoints
  search: {
    path: `/search`,
    returnas: {} as searchresult
  },
  delete: {
    path: `/results/delete`,
    returnas: "" as string
  },
  deleteBulk: {
    path: `/results/delete-bulk`,
    returnas: {} as { ok: boolean; count: number }
  },
  submit: {
    path: `/submit`,
    returnas: "" as string
  },
  submitsingle: {
    path: `/submit/single`,
    returnas: {} as detail
  }
};

type Endpoints = typeof endpoints;
type EndpointReturnType<K extends keyof Endpoints> = Endpoints[K]['returnas'];

const replacePathParams = (path: string, params?: Record<string, string | number | boolean>): [string, Record<string, string | number | boolean>] => {
  if (!params) return [path, {}];

  const paramRegex = /:([a-zA-Z0-9_]+)/g;
  const missingParams: string[] = [];
  const remainingParams = { ...params }; // Create a copy of the params object to modify

  // Replace all `:param` placeholders with the corresponding values from params
  const newPath = path.replace(paramRegex, (match, paramName) => {
    if (paramName in remainingParams) {
      const value = remainingParams[paramName];
      delete remainingParams[paramName];
      return encodeURIComponent(value.toString());
    } else {
      missingParams.push(paramName);
      return match;
    }
  });

  // If any required params were missing, throw an error
  if (missingParams.length > 0) {
    throw new Error(`Missing required parameters: ${missingParams.join(', ')}`);
  }

  return [newPath, remainingParams];
};

const serializeParams = (params: Record<string, string | number | boolean>) => {
  const query = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    query.append(key, value.toString());
  });
  return query.toString() ? `?${query.toString()}` : '';
};

const get = async <K extends keyof Endpoints>(
  endpointKey: K,
  params?: Record<string, string | number | boolean>,
  raw: boolean = false
): Promise<EndpointReturnType<K>> => {

  const endpoint = endpoints[endpointKey];
  const [pathWithParams, remainingParams] = replacePathParams(endpoint.path, params);
  const queryString = remainingParams ? serializeParams(remainingParams) : '';

  const res = await fetch(`${endpoints.base.path}${pathWithParams}${queryString}`);

  if (!res.ok) throw new Error(`HTTP Error: ${res.status}`);

  if (raw) return await res.text() as unknown as EndpointReturnType<K>;
  return await res.json() as EndpointReturnType<K>;
};

const post = async <K extends keyof Endpoints>(
  endpointKey: K,
  data?: unknown,
  pathParams?: Record<string, string | number | boolean>
): Promise<EndpointReturnType<K>> => {

  const endpoint = endpoints[endpointKey];
  const [pathWithParams] = replacePathParams(endpoint.path, pathParams);
  const res = await fetch(`${endpoints.base.path}${pathWithParams}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data)
  });

  if (!res.ok) throw new Error(`HTTP Error: ${res.status}`);

  return await res.json() as EndpointReturnType<K>;
};

export { endpoints, get, post };