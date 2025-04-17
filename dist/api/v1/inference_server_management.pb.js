/* eslint-disable */
// @ts-nocheck
/*
* This file is a generated Typescript file for GRPC Gateway, DO NOT MODIFY
*/
import * as fm from "../../fetch.pb";
export class InferenceService {
    static GetInferenceStatus(req, initReq) {
        return fm.fetchReq(`/v1/inference/status?${fm.renderURLSearchParams(req, [])}`, Object.assign(Object.assign({}, initReq), { method: "GET" }));
    }
    static ActivateModel(req, initReq) {
        return fm.fetchReq(`/v1/inference/models/${req["id"]}:activate`, Object.assign(Object.assign({}, initReq), { method: "POST" }));
    }
    static DeactivateModel(req, initReq) {
        return fm.fetchReq(`/v1/inference/models/${req["id"]}:deactivate`, Object.assign(Object.assign({}, initReq), { method: "POST" }));
    }
}
