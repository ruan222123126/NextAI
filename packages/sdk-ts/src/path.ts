import type { APIPath } from "./generated.js";

type Primitive = string | number | boolean;
type QueryValue = Primitive | null | undefined;
type QueryInputValue = QueryValue | QueryValue[];

type ExtractPathParamNames<S extends string> =
  S extends `${string}{${infer Name}}${infer Rest}` ? Name | ExtractPathParamNames<Rest> : never;

export type PathParams<P extends string> = [ExtractPathParamNames<P>] extends [never]
  ? Record<string, never>
  : { [K in ExtractPathParamNames<P>]: Primitive };

export function fillPath<P extends APIPath>(template: P, params: PathParams<P>): string {
  return template.replace(/\{([^}]+)\}/g, (_matched, key: string) => {
    const value = params[key as keyof PathParams<P>];
    if (value === undefined || value === null) {
      throw new Error(`missing path param: ${key}`);
    }
    return encodeURIComponent(String(value));
  });
}

export function appendQuery(path: string, query: Record<string, QueryInputValue>): string {
  const entries: Array<[string, string]> = [];
  for (const [key, value] of Object.entries(query)) {
    if (Array.isArray(value)) {
      for (const item of value) {
        if (item === undefined || item === null) {
          continue;
        }
        entries.push([key, String(item)]);
      }
      continue;
    }
    if (value === undefined || value === null) {
      continue;
    }
    entries.push([key, String(value)]);
  }
  if (entries.length === 0) {
    return path;
  }
  const search = new URLSearchParams(entries);
  return `${path}?${search.toString()}`;
}
