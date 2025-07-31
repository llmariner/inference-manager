/* eslint-disable */
// @ts-nocheck
/*
* This file is a generated Typescript file for GRPC Gateway, DO NOT MODIFY
*/

import * as fm from "../../fetch.pb"
export type EngineStatusModel = {
  id?: string
  is_ready?: boolean
  in_progress_task_count?: number
  gpu_allocated?: number
  is_dynamically_loaded_lora?: boolean
}

export type EngineStatus = {
  engine_id?: string
  ready?: boolean
  models?: EngineStatusModel[]
  cluster_id?: string
}

export type InferenceStatus = {
  cluster_statuses?: ClusterStatus[]
  task_status?: TaskStatus
}

export type TaskStatus = {
  in_progress_task_counts?: {[key: string]: number}
}

export type ClusterStatus = {
  id?: string
  name?: string
  engine_statuses?: EngineStatus[]
  model_count?: number
  in_progress_task_count?: number
  gpu_allocated?: number
}

export type GetInferenceStatusRequest = {
}

export class InferenceService {
  static GetInferenceStatus(req: GetInferenceStatusRequest, initReq?: fm.InitReq): Promise<InferenceStatus> {
    return fm.fetchReq<GetInferenceStatusRequest, InferenceStatus>(`/v1/inference/status?${fm.renderURLSearchParams(req, [])}`, {...initReq, method: "GET"})
  }
}