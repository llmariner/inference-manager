/* eslint-disable */
// @ts-nocheck
/*
* This file is a generated Typescript file for GRPC Gateway, DO NOT MODIFY
*/
var __awaiter = (this && this.__awaiter) || function (thisArg, _arguments, P, generator) {
    function adopt(value) { return value instanceof P ? value : new P(function (resolve) { resolve(value); }); }
    return new (P || (P = Promise))(function (resolve, reject) {
        function fulfilled(value) { try { step(generator.next(value)); } catch (e) { reject(e); } }
        function rejected(value) { try { step(generator["throw"](value)); } catch (e) { reject(e); } }
        function step(result) { result.done ? resolve(result.value) : adopt(result.value).then(fulfilled, rejected); }
        step((generator = generator.apply(thisArg, _arguments || [])).next());
    });
};
var __rest = (this && this.__rest) || function (s, e) {
    var t = {};
    for (var p in s) if (Object.prototype.hasOwnProperty.call(s, p) && e.indexOf(p) < 0)
        t[p] = s[p];
    if (s != null && typeof Object.getOwnPropertySymbols === "function")
        for (var i = 0, p = Object.getOwnPropertySymbols(s); i < p.length; i++) {
            if (e.indexOf(p[i]) < 0 && Object.prototype.propertyIsEnumerable.call(s, p[i]))
                t[p[i]] = s[p[i]];
        }
    return t;
};
export function fetchReq(path, init) {
    const _a = init || {}, { pathPrefix } = _a, req = __rest(_a, ["pathPrefix"]);
    const url = pathPrefix ? `${pathPrefix}${path}` : path;
    return fetch(url, req).then(r => r.json().then((body) => {
        if (!r.ok) {
            throw body;
        }
        return body;
    }));
}
/**
 * fetchStreamingRequest is able to handle grpc-gateway server side streaming call
 * it takes NotifyStreamEntityArrival that lets users respond to entity arrival during the call
 * all entities will be returned as an array after the call finishes.
 **/
export function fetchStreamingRequest(path, callback, init) {
    return __awaiter(this, void 0, void 0, function* () {
        const _a = init || {}, { pathPrefix } = _a, req = __rest(_a, ["pathPrefix"]);
        const url = pathPrefix ? `${pathPrefix}${path}` : path;
        const result = yield fetch(url, req);
        // needs to use the .ok to check the status of HTTP status code
        // http other than 200 will not throw an error, instead the .ok will become false.
        // see https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API/Using_Fetch#
        if (!result.ok) {
            const resp = yield result.json();
            const errMsg = resp.error && resp.error.message ? resp.error.message : "";
            throw new Error(errMsg);
        }
        if (!result.body) {
            throw new Error("response doesnt have a body");
        }
        yield result.body
            .pipeThrough(new TextDecoderStream())
            .pipeThrough(getNewLineDelimitedJSONDecodingStream())
            .pipeTo(getNotifyEntityArrivalSink((e) => {
            if (callback) {
                callback(e);
            }
        }));
        // wait for the streaming to finish and return the success respond
        return;
    });
}
/**
 * getNewLineDelimitedJSONDecodingStream returns a TransformStream that's able to handle new line delimited json stream content into parsed entities
 */
function getNewLineDelimitedJSONDecodingStream() {
    return new TransformStream({
        start(controller) {
            controller.buf = '';
            controller.pos = 0;
        },
        transform(chunk, controller) {
            if (controller.buf === undefined) {
                controller.buf = '';
            }
            if (controller.pos === undefined) {
                controller.pos = 0;
            }
            controller.buf += chunk;
            while (controller.pos < controller.buf.length) {
                if (controller.buf[controller.pos] === '\n') {
                    const line = controller.buf.substring(0, controller.pos);
                    const response = JSON.parse(line);
                    controller.enqueue(response.result);
                    controller.buf = controller.buf.substring(controller.pos + 1);
                    controller.pos = 0;
                }
                else {
                    ++controller.pos;
                }
            }
        }
    });
}
/**
 * getNotifyEntityArrivalSink takes the NotifyStreamEntityArrival callback and return
 * a sink that will call the callback on entity arrival
 * @param notifyCallback
 */
function getNotifyEntityArrivalSink(notifyCallback) {
    return new WritableStream({
        write(entity) {
            notifyCallback(entity);
        }
    });
}
/**
 * Checks if given value is a plain object
 * Logic copied and adapted from below source:
 * https://github.com/char0n/ramda-adjunct/blob/master/src/isPlainObj.js
 * @param  {unknown} value
 * @return {boolean}
 */
function isPlainObject(value) {
    const isObject = Object.prototype.toString.call(value).slice(8, -1) === "Object";
    const isObjLike = value !== null && isObject;
    if (!isObjLike || !isObject) {
        return false;
    }
    const proto = Object.getPrototypeOf(value);
    const hasObjectConstructor = typeof proto === "object" &&
        proto.constructor === Object.prototype.constructor;
    return hasObjectConstructor;
}
/**
 * Checks if given value is of a primitive type
 * @param  {unknown} value
 * @return {boolean}
 */
function isPrimitive(value) {
    return ["string", "number", "boolean"].some(t => typeof value === t);
}
/**
 * Checks if given primitive is zero-value
 * @param  {Primitive} value
 * @return {boolean}
 */
function isZeroValuePrimitive(value) {
    return value === false || value === 0 || value === "";
}
/**
 * Flattens a deeply nested request payload and returns an object
 * with only primitive values and non-empty array of primitive values
 * as per https://github.com/googleapis/googleapis/blob/master/google/api/http.proto
 * @param  {RequestPayload} requestPayload
 * @param  {String} path
 * @return {FlattenedRequestPayload>}
 */
function flattenRequestPayload(requestPayload, path = "") {
    return Object.keys(requestPayload).reduce((acc, key) => {
        const value = requestPayload[key];
        const newPath = path ? [path, key].join(".") : key;
        const isNonEmptyPrimitiveArray = Array.isArray(value) &&
            value.every(v => isPrimitive(v)) &&
            value.length > 0;
        const isNonZeroValuePrimitive = isPrimitive(value) && !isZeroValuePrimitive(value);
        let objectToMerge = {};
        if (isPlainObject(value)) {
            objectToMerge = flattenRequestPayload(value, newPath);
        }
        else if (isNonZeroValuePrimitive || isNonEmptyPrimitiveArray) {
            objectToMerge = { [newPath]: value };
        }
        return Object.assign(Object.assign({}, acc), objectToMerge);
    }, {});
}
/**
 * Renders a deeply nested request payload into a string of URL search
 * parameters by first flattening the request payload and then removing keys
 * which are already present in the URL path.
 * @param  {RequestPayload} requestPayload
 * @param  {string[]} urlPathParams
 * @return {string}
 */
export function renderURLSearchParams(requestPayload, urlPathParams = []) {
    const flattenedRequestPayload = flattenRequestPayload(requestPayload);
    const urlSearchParams = Object.keys(flattenedRequestPayload).reduce((acc, key) => {
        // key should not be present in the url path as a parameter
        const value = flattenedRequestPayload[key];
        if (urlPathParams.find(f => f === key)) {
            return acc;
        }
        return Array.isArray(value)
            ? [...acc, ...value.map(m => [key, m.toString()])]
            : (acc = [...acc, [key, value.toString()]]);
    }, []);
    return new URLSearchParams(urlSearchParams).toString();
}
