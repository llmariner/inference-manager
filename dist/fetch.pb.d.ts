export interface InitReq extends RequestInit {
    pathPrefix?: string;
}
export declare function fetchReq<I, O>(path: string, init?: InitReq): Promise<O>;
export type NotifyStreamEntityArrival<T> = (resp: T) => void;
/**
 * fetchStreamingRequest is able to handle grpc-gateway server side streaming call
 * it takes NotifyStreamEntityArrival that lets users respond to entity arrival during the call
 * all entities will be returned as an array after the call finishes.
 **/
export declare function fetchStreamingRequest<S, R>(path: string, callback?: NotifyStreamEntityArrival<R>, init?: InitReq): Promise<void>;
type RequestPayload = Record<string, unknown>;
/**
 * Renders a deeply nested request payload into a string of URL search
 * parameters by first flattening the request payload and then removing keys
 * which are already present in the URL path.
 * @param  {RequestPayload} requestPayload
 * @param  {string[]} urlPathParams
 * @return {string}
 */
export declare function renderURLSearchParams<T extends RequestPayload>(requestPayload: T, urlPathParams?: string[]): string;
export {};
