import type { APIPath } from "./generated.js";
type Primitive = string | number | boolean;
type QueryValue = Primitive | null | undefined;
type QueryInputValue = QueryValue | QueryValue[];
type ExtractPathParamNames<S extends string> = S extends `${string}{${infer Name}}${infer Rest}` ? Name | ExtractPathParamNames<Rest> : never;
export type PathParams<P extends string> = [ExtractPathParamNames<P>] extends [never] ? Record<string, never> : {
    [K in ExtractPathParamNames<P>]: Primitive;
};
export declare function fillPath<P extends APIPath>(template: P, params: PathParams<P>): string;
export declare function appendQuery(path: string, query: Record<string, QueryInputValue>): string;
export {};
