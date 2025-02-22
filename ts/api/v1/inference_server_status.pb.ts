/* eslint-disable */
// @ts-nocheck
/*
* This file is a generated Typescript file for GRPC Gateway, DO NOT MODIFY
*/

import * as fm from "../../fetch.pb"
import * as LlmarinerInferenceServerV1Inference_server_worker from "./inference_server_worker.pb"
export type InferenceStatus = {
  clusterStatuses?: ClusterStatus[]
  taskStatus?: TaskStatus
}

export type TaskStatus = {
  inProgressTaskCounts?: {[key: string]: number}
}

export type ClusterStatus = {
  id?: string
  name?: string
  engineStatuses?: LlmarinerInferenceServerV1Inference_server_worker.EngineStatus[]
  modelCount?: number
  inProgressTaskCount?: number
  gpuAllocated?: number
}

export type GetInferenceStatusRequest = {
}

export class InferenceService {
  static GetInferenceStatus(req: GetInferenceStatusRequest, initReq?: fm.InitReq): Promise<InferenceStatus> {
    return fm.fetchReq<GetInferenceStatusRequest, InferenceStatus>(`/v1/inference/status?${fm.renderURLSearchParams(req, [])}`, {...initReq, method: "GET"})
  }
}